package writebackhost_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type bindingSource struct {
	library.Repository
	track library.Track
}

func (s bindingSource) FindTrack(context.Context, string, string) (library.Track, error) {
	return s.track, nil
}

type bindingWrites struct {
	writebackhost.Repository
	queued int
}

func (*bindingWrites) ReplayWriteback(context.Context, string, writebackhost.Request, string) (writebackhost.Job, bool, error) {
	return writebackhost.Job{}, false, nil
}

func (r *bindingWrites) QueueWriteback(_ context.Context, owner string, input writebackhost.Request, _ writeback.Target, key string) (writebackhost.Job, bool, error) {
	r.queued++
	return writebackhost.Job{ID: key, Owner: owner, Request: input, Status: "queued"}, false, nil
}

func TestWritebackSubmitRejectsChangedDirectoryBinding(t *testing.T) {
	ctx := context.Background()
	payload, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	createRoot := func() (*sourceaccess.Access, sourceaccess.RootConfig) {
		t.Helper()
		path := t.TempDir()
		if err := os.WriteFile(filepath.Join(path, "track.flac"), payload, 0600); err != nil {
			t.Fatal(err)
		}
		access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: path}})
		if err != nil {
			t.Fatal(err)
		}
		return access, access.Roots()[0]
	}
	original, originalRoot := createRoot()
	replacement, replacementRoot := createRoot()
	entry, err := original.Inspect("music", "track.flac")
	if err != nil {
		t.Fatal(err)
	}
	track := library.Track{ID: "track", Owner: "owner", ProviderID: "provider", RootID: "music", Path: "track.flac", AssetID: "asset", Version: entry.ObservedVersion, Size: entry.SizeBytes, Availability: "available"}
	runtime := library.Runtime{ID: "provider", Owner: "owner", Worker: &pluginhost.Worker{Access: original}}
	probe := func(context.Context, io.ReadSeeker, int64) (library.Observation, error) {
		return library.Observation{}, nil
	}
	lib := library.New(bindingSource{track: track}, jobs.New(nil), []library.Runtime{runtime}, probe)
	journal, err := writeback.OpenJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	grant := writeback.Root{Owner: "owner", InstanceID: "provider", RootID: "music", Path: originalRoot.Path, Identity: originalRoot.ExpectedIdentity, Fields: []string{"lyrics"}}
	request := writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Version, Fields: []writebackhost.Field{{Name: "lyrics", Language: "und"}}}
	createService := func(root writeback.Root) (*writebackhost.Service, *bindingWrites) {
		t.Helper()
		repo := &bindingWrites{}
		service, err := writebackhost.New(repo, lib, nil, journal, []library.Runtime{runtime}, writebackhost.Config{Policy: writeback.Policy{Enabled: true, Roots: []writeback.Root{root}}, Probe: probe})
		if err != nil {
			t.Fatal(err)
		}
		return service, repo
	}
	service, repo := createService(grant)
	if job, _, err := service.Submit(ctx, "owner", request, "original-root-submit"); err != nil || job.Status != "queued" || repo.queued != 1 {
		t.Fatalf("original directory grant did not admit a submission: %+v %v", job, err)
	}
	runtime.Worker = &pluginhost.Worker{Access: replacement}
	if err = lib.UpdateRuntime(runtime); err != nil {
		t.Fatal(err)
	}
	_, _, err = service.Submit(ctx, "owner", request, "changed-root-submit")
	var rejected *domain.Error
	if !errors.As(err, &rejected) || rejected.Code != "ACCESS_DENIED" || repo.queued != 1 {
		t.Fatalf("reused root ID followed a new directory: queued=%d error=%v", repo.queued, err)
	}
	grant.Path = replacementRoot.Path
	service, repo = createService(grant)
	if _, _, err = service.Submit(ctx, "owner", request, "wrong-identity-submit"); err == nil || repo.queued != 0 {
		t.Fatalf("matching path accepted a different directory identity: queued=%d error=%v", repo.queued, err)
	}
	for _, root := range []sourceaccess.RootConfig{originalRoot, replacementRoot} {
		actual, err := os.ReadFile(filepath.Join(root.Path, "track.flac"))
		if err != nil || !bytes.Equal(actual, payload) {
			t.Fatal("submission changed a source file", err)
		}
	}
}
