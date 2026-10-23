package writebackhost_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/tags/flac"
	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/tagformat"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func (s bindingSource) ProviderScanState(context.Context, string, string) string { return "idle" }

type capabilityWrites struct {
	writebackhost.Repository
	work      writebackhost.Work
	queued    int
	taken     atomic.Bool
	mutations atomic.Int64
	settled   chan [2]string
}

func (*capabilityWrites) ReplayWriteback(context.Context, string, writebackhost.Request, string) (writebackhost.Job, bool, error) {
	return writebackhost.Job{}, false, nil
}
func (r *capabilityWrites) QueueWriteback(_ context.Context, owner string, request writebackhost.Request, target writeback.Target, key string) (writebackhost.Job, bool, error) {
	r.queued++
	r.work = writebackhost.Work{Job: writebackhost.Job{ID: key, Owner: owner, Request: request, Status: "queued"}, Target: target}
	return r.work.Job, false, nil
}
func (r *capabilityWrites) GetWriteback(context.Context, string, string) (writebackhost.Job, error) {
	return r.work.Job, nil
}
func (r *capabilityWrites) NextWriteback(context.Context) (writebackhost.Work, error) {
	if r.taken.Swap(true) {
		return writebackhost.Work{}, domain.Fail("NOT_FOUND", "No queued writeback")
	}
	return r.work, nil
}
func (*capabilityWrites) RecoverWritebackQueue(context.Context, map[string]bool) error { return nil }
func (r *capabilityWrites) BeginWriteback(context.Context, string, string, writeback.Target, int64) error {
	r.mutations.Add(1)
	return errors.New("unexpected writeback mutation")
}
func (r *capabilityWrites) CheckWriteback(context.Context, string, string, writeback.Target, int64) (bool, error) {
	r.mutations.Add(1)
	return false, errors.New("unexpected writeback mutation")
}
func (r *capabilityWrites) SetWritebackState(_ context.Context, _, _, status, code string) error {
	select {
	case r.settled <- [2]string{status, code}:
	default:
	}
	return nil
}

type capabilityJournal struct {
	writeback.Journal
	record *writeback.Record
}

func (j capabilityJournal) Load(context.Context, string) (writeback.Record, error) {
	if j.record != nil {
		return *j.record, nil
	}
	return writeback.Record{}, domain.Fail("NOT_FOUND", "Writeback journal not found")
}
func (j capabilityJournal) Pending(context.Context, string, int) ([]writeback.Record, error) {
	if j.record != nil {
		return []writeback.Record{*j.record}, nil
	}
	return nil, nil
}

// 此夹具只验证协调准入，空正文也通过插件校验，确保拒绝来自 Provider 能力检查。
type capabilityFormat struct{ tagformat.Format }

func (capabilityFormat) ValidatePatch(tagformat.Patch) error { return nil }

func TestWritebackRequiresStableIdentityBeforeQueueAndAfterReload(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		name := "queued"
		if recovery {
			name = "recovery"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			body, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/lossless.flac"))
			if err != nil {
				t.Fatal(err)
			}
			path := t.TempDir()
			file := filepath.Join(path, "track.flac")
			if err = os.WriteFile(file, body, 0600); err != nil {
				t.Fatal(err)
			}
			access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: path}})
			if err != nil {
				t.Fatal(err)
			}
			entry, err := access.Inspect("music", "track.flac")
			if err != nil {
				t.Fatal(err)
			}
			track := library.Track{ID: "track", Owner: "owner", ProviderID: "provider", RootID: "music", Path: "track.flac", AssetID: "asset", Version: entry.ObservedVersion, Size: entry.SizeBytes, Availability: "available"}
			runtime := library.Runtime{ID: "provider", Owner: "owner", Worker: &pluginhost.Worker{Access: access, Manifest: pluginproto.Manifest{Capabilities: []string{"library.packages"}}}, RootNames: map[string]string{"music": "Music"}, WriteBack: map[string]library.RootWriteBack{"music": {Enabled: true, Available: true, Formats: []string{"flac"}, Fields: []string{"lyrics"}}}}
			probe := func(context.Context, io.ReadSeeker, int64) (library.Observation, error) {
				return library.Observation{}, nil
			}
			queue := jobs.New(nil)
			defer queue.Close()
			lib := library.New(bindingSource{track: track}, queue, []library.Runtime{runtime}, probe)
			root := access.Roots()[0]
			request := writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Version, Fields: []writebackhost.Field{{Name: "lyrics", Language: "und"}}}
			repo := &capabilityWrites{settled: make(chan [2]string, 1)}
			journal := &capabilityJournal{}
			cfg := writebackhost.Config{Policy: writeback.Policy{Enabled: true, Roots: []writeback.Root{{Owner: "owner", InstanceID: "provider", RootID: "music", Path: path, Identity: root.ExpectedIdentity, Fields: []string{"lyrics"}}}}, Probe: probe, TagFormat: capabilityFormat{flac.New()}}
			service, err := writebackhost.New(repo, lib, nil, journal, []library.Runtime{runtime}, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = service.Submit(ctx, "owner", request, domain.NewID()); pluginproto.ErrorCode(err) != "UNSUPPORTED" || repo.queued != 0 {
				t.Fatalf("provider without rebinding capability entered the queue: queued=%d error=%v", repo.queued, err)
			}
			if lib.Providers(ctx, "owner")[0].Roots[0].WriteBack.Available {
				t.Fatal("provider without rebinding capability advertised writeback")
			}
			runtime.Worker = &pluginhost.Worker{Access: access, Manifest: pluginproto.Manifest{Capabilities: []string{directoryidentity.Capability}}}
			if err = lib.UpdateRuntime(runtime); err != nil {
				t.Fatal(err)
			}
			job, _, err := service.Submit(ctx, "owner", request, domain.NewID())
			if err != nil || repo.queued != 1 {
				t.Fatal("capable provider did not queue writeback", err)
			}
			if !lib.Providers(ctx, "owner")[0].Roots[0].WriteBack.Available {
				t.Fatal("capable provider lost its writeback declaration")
			}
			if recovery {
				journal.record = &writeback.Record{ID: job.ID, Owner: job.Owner, Target: repo.work.Target, RootIdentity: root.ExpectedIdentity, Fields: []string{"lyrics"}, Stage: "written"}
				repo.work.Job.Status = "recovery"
				repo.taken.Store(true)
			}
			runtime.Worker = &pluginhost.Worker{Access: access, Manifest: pluginproto.Manifest{Capabilities: []string{"library.packages"}}}
			if err = lib.UpdateRuntime(runtime); err != nil {
				t.Fatal(err)
			}
			if lib.Providers(ctx, "owner")[0].Roots[0].WriteBack.Available {
				t.Fatal("reloaded provider retained stale writeback availability")
			}
			if err = service.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			select {
			case outcome := <-repo.settled:
				want := "failed"
				if recovery {
					want = "recovery"
				}
				if outcome != [2]string{want, "UNSUPPORTED"} || repo.mutations.Load() != 0 {
					t.Fatalf("reloaded provider reached mutation or recovery: %+v mutations=%d", outcome, repo.mutations.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("writeback did not reject the reloaded provider")
			}
			service.Close()
			if lib.WritebackPending("owner", "provider", job.ID) != recovery {
				t.Fatal("writeback created a new barrier or removed an unresolved recovery barrier")
			}
			actual, err := os.ReadFile(file)
			files, listErr := os.ReadDir(path)
			if err != nil || listErr != nil || !bytes.Equal(actual, body) || len(files) != 1 {
				t.Fatal("rejected provider changed source files", err, listErr)
			}
		})
	}
}
