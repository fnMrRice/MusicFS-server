package writeback_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

type host struct {
	mu           sync.Mutex
	target       writeback.Target
	revision     int64
	locked, busy bool
	last         writeback.Result
	commits      int
	failCommit   bool
}
type lease struct {
	h   *host
	job string
}

func (h *host) Acquire(_ context.Context, owner string, target writeback.Target, revision int64, id string) (writeback.Lease, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if owner != "admin" || target != h.target || revision != h.revision {
		return nil, domain.Fail("RESOURCE_CHANGED", "Host binding changed")
	}
	if h.locked || h.busy {
		return nil, domain.Fail("CONFLICT", "Source is in use")
	}
	h.locked = true
	return &lease{h, id}, nil
}
func (h *host) Recover(ctx context.Context, r writeback.Record) (writeback.Lease, error) {
	h.mu.Lock()
	done := h.last.JobID == r.ID
	if done && !h.locked && !h.busy {
		h.locked = true
		h.mu.Unlock()
		return &lease{h, r.ID}, nil
	}
	h.mu.Unlock()
	return h.Acquire(ctx, r.Owner, r.Target, r.SelectionRevision, r.ID)
}
func (l *lease) Check(ctx context.Context) error { return ctx.Err() }
func (l *lease) Commit(_ context.Context, result writeback.Result) error {
	l.h.mu.Lock()
	defer l.h.mu.Unlock()
	if l.h.failCommit {
		return errors.New("injected database failure")
	}
	if l.h.last.JobID == l.job {
		return nil
	}
	l.h.last = result
	l.h.commits++
	if result.Changed {
		l.h.target.FileIdentity = result.New.Identity
		l.h.target.AssetVersion = result.New.Version
		l.h.target.AssetID = domain.NewID()
	}
	return nil
}
func (l *lease) Close() error { l.h.mu.Lock(); defer l.h.mu.Unlock(); l.h.locked = false; return nil }

func requestFor(t *testing.T, rootPath, relative string) (writeback.Policy, writeback.Request, *host) {
	t.Helper()
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: rootPath}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := a.Inspect("music", relative)
	if err != nil {
		t.Fatal(err)
	}
	target := writeback.Target{SourceTrackID: "stable-track", AssetID: "old-asset", AssetVersion: entry.ObservedVersion, InstanceID: "local", RootID: "music", RelativePath: filepath.ToSlash(relative), FileIdentity: entry.FileIdentity}
	p := writeback.Policy{Enabled: true, Roots: []writeback.Root{{Owner: "admin", InstanceID: "local", RootID: "music", Path: rootPath, Identity: a.Roots()[0].ExpectedIdentity, Fields: []string{"title", "artists", "album", "lyrics", "artwork"}}}}
	r := writeback.Request{ID: domain.NewID(), Target: target, SelectionRevision: 7, Patch: writeback.Patch{Comments: map[string][]string{"title": {"新的标题"}, "lyrics": {"Line one\nLine two"}}, Artwork: picture(t), Overwrite: true}}
	return p, r, &host{target: target, revision: 7}
}

type failingJournal struct {
	writeback.Journal
	stage string
	after bool
	fired bool
}

func (j *failingJournal) Save(ctx context.Context, from string, r writeback.Record) error {
	if !j.fired && r.Stage == j.stage {
		j.fired = true
		if j.after {
			if err := j.Journal.Save(ctx, from, r); err != nil {
				return err
			}
		}
		return errors.New("injected journal interruption")
	}
	return j.Journal.Save(ctx, from, r)
}
