package writebackhost

import (
	"context"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

type coordinator struct{ s *Service }
type lease struct {
	s            *Service
	gate         *library.ExclusiveLease
	owner, id    string
	target       writeback.Target
	revision     int64
	ctx          context.Context
	probeFn      library.Probe
	releaseProbe func()
}

func (l *lease) Context() context.Context { return l.ctx }

func (c coordinator) Acquire(ctx context.Context, owner string, target writeback.Target, revision int64, id string) (writeback.Lease, error) {
	return c.s.acquire(ctx, owner, target, revision, id, false)
}
func (c coordinator) Recover(ctx context.Context, r writeback.Record) (writeback.Lease, error) {
	return c.s.acquire(ctx, r.Owner, r.Target, r.SelectionRevision, r.ID, true)
}

func (s *Service) acquire(ctx context.Context, owner string, target writeback.Target, revision int64, id string, recovering bool) (writeback.Lease, error) {
	g, err := s.lib.TryExclusive(ctx, owner, target.InstanceID, id)
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) && de.Code == "CONFLICT" {
			return nil, errBusy
		}
		return nil, err
	}
	// 排队后可能发生重载；持久任务与恢复均重新检查本次排他租约绑定的实现。
	if err = g.CheckCapability(ctx, directoryidentity.Capability); err != nil {
		g.Close()
		return nil, err
	}
	access, err := g.Access()
	if err == nil {
		_, err = s.boundRoot(owner, target.InstanceID, target.RootID, access)
	}
	if err != nil {
		g.Close()
		return nil, err
	}
	ctx, probe, release := g.Context(), s.cfg.Probe, func() {}
	if s.cfg.ProbeSession != nil {
		ctx, probe, release, err = s.cfg.ProbeSession(ctx)
		if err != nil {
			g.Close()
			return nil, err
		}
	}
	l := &lease{s: s, gate: g, owner: owner, id: id, target: target, revision: revision, ctx: ctx, probeFn: probe, releaseProbe: release}
	if recovering {
		_, err = s.repo.CheckWriteback(ctx, owner, id, target, revision)
	} else {
		err = s.repo.BeginWriteback(ctx, owner, id, target, revision)
	}
	if err != nil {
		release()
		g.Close()
		return nil, err
	}
	if err = g.Block(); err != nil {
		release()
		g.Close()
		return nil, err
	}
	return l, nil
}

func (l *lease) Check(ctx context.Context) error {
	if err := l.ctx.Err(); err != nil {
		return err
	}
	if err := l.gate.Check(ctx); err != nil {
		return err
	}
	_, err := l.s.repo.CheckWriteback(ctx, l.owner, l.id, l.target, l.revision)
	return err
}

func (l *lease) Commit(ctx context.Context, result writeback.Result) error {
	done, err := l.s.repo.CheckWriteback(ctx, l.owner, l.id, l.target, l.revision)
	if err != nil || done {
		return err
	}
	w, err := l.s.repo.WritebackWork(ctx, l.owner, l.id)
	if err != nil {
		return err
	}
	if result.JobID != l.id || result.SourceTrackID != l.target.SourceTrackID {
		return domain.Fail("CONFLICT", "Writeback result belongs to another operation")
	}
	var observed library.Observation
	if result.Changed {
		root, err := l.s.grant(l.owner, l.target.InstanceID, l.target.RootID, w.Job.Request.Fields)
		if err != nil {
			return err
		}
		provider, err := l.gate.RebindLocalTrack(ctx, pluginhost.LocalRebind{JobID: l.id, RootID: l.target.RootID, RootIdentity: root.Identity, RelativePath: l.target.RelativePath, ExternalID: w.ExternalID, OldIdentity: result.Old.Identity, OldVersion: result.Old.Version, NewIdentity: result.New.Identity, NewVersion: result.New.Version})
		if err != nil {
			return err
		}
		observed, err = l.probe(ctx, provider)
		if err != nil {
			return err
		}
	} else {
		t, err := l.s.lib.Track(ctx, l.owner, l.target.SourceTrackID)
		if err != nil {
			return err
		}
		observed = library.Observation{Observation: pluginproto.Observation{ExternalID: w.ExternalID, RootID: t.RootID, RelativePath: t.Path, ObservedVersion: t.Version, DisplayName: t.DisplayName, SizeBytes: t.Size, MIME: t.MIME, Codec: t.Codec, DurationMs: t.DurationMs}, Container: t.Container, Title: t.Title, Artists: t.Artists, Album: t.Album, Genre: t.Genre, Year: t.Year, SampleRateHz: t.SampleRateHz}
	}
	_, err = l.s.repo.CommitWriteback(ctx, l.owner, l.id, result, observed)
	return err
}

func (l *lease) probe(ctx context.Context, provider pluginproto.Observation) (library.Observation, error) {
	var result library.Observation
	if l.probeFn == nil {
		return result, domain.Fail("UNSUPPORTED", "Source writeback requires a metadata probe")
	}
	access, err := l.gate.Access()
	if err != nil {
		return result, err
	}
	scope := sourceaccess.Scope{UserID: l.owner, InstanceID: l.target.InstanceID, TaskID: "writeback-" + l.id, AssetID: l.target.AssetID}
	ref, err := access.Register(scope, l.target.RootID, l.target.RelativePath, provider.ObservedVersion, time.Minute)
	if err != nil {
		return result, err
	}
	defer access.Revoke(scope.TaskID)
	input, err := access.Open(ctx, scope, ref.ResourceRef)
	if err != nil {
		return result, err
	}
	defer input.Close()
	result, err = l.probeFn(ctx, input, provider.SizeBytes)
	if err != nil {
		return result, err
	}
	// Provider identity/path/version come from the guarded actual installed file;
	// codec, duration, tags and evidence come from the new independent probe.
	result.ExternalID = provider.ExternalID
	result.RootID = provider.RootID
	result.RelativePath = provider.RelativePath
	result.SourceRef = provider.SourceRef
	result.ObservedVersion = provider.ObservedVersion
	result.DisplayName = provider.DisplayName
	result.SizeBytes = provider.SizeBytes
	if result.MIME == "" {
		result.MIME = provider.MIME
	}
	result.ProviderObservation = &provider
	result.ProbeStatus = "succeeded"
	return result, nil
}

func (l *lease) Close() error {
	defer l.releaseProbe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := l.s.journal.Load(ctx, l.id)
	var de *domain.Error
	if (err == nil && (r.Stage == "committed" || r.Stage == "aborted")) || (errors.As(err, &de) && de.Code == "NOT_FOUND") {
		l.gate.ClearBarrier()
	} else {
		l.gate.Block()
	}
	return l.gate.Close()
}
