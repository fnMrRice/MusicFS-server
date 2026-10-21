package enrichment_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type fixture struct {
	service         *enrichmentworker.Service
	store           *sqlite.Store
	content         *mediastore.ContentService
	lib             *library.Service
	runtimes        []enrichmentworker.Runtime
	tracks          []library.Track
	root, mediaRoot string
}

func setup(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	root := filepath.Join(dir, "music")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	audio := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00[00:01]Embedded fixture lyrics")})
	for _, name := range []string{"good.mp3", "other.mp3"} {
		if err := os.WriteFile(filepath.Join(root, name), audio, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "good.lrc"), []byte("[00:01]旁置合成歌词"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cover.png"), testkit.ScraperPNG(), 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(dir, "database.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	_, _, err = store.Enqueue(ctx, "admin", "local", testkit.ScanPlan("root"), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	observations := []library.Observation{}
	for _, name := range []string{"good.mp3", "other.mp3"} {
		e, err := access.Inspect("root", name)
		if err != nil {
			t.Fatal(err)
		}
		observations = append(observations, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: "root", RelativePath: name, ObservedVersion: e.ObservedVersion, SizeBytes: e.SizeBytes, MIME: e.MIME, DisplayName: name}, Artists: []string{}})
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, observations)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, 2, nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, "admin", "local", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	lib := library.New(store, jobs.New(store), []library.Runtime{{ID: "local", Owner: "admin", Worker: &pluginhost.Worker{Access: access}}}, nil)
	mediaRoot := filepath.Join(dir, "media")
	files, err := mediastore.OpenFiles(mediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	content := mediastore.NewContent(store, store, files)
	workerPath := filepath.Join(dir, "scraper-worker")
	if runtime.GOOS == "windows" {
		workerPath += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", workerPath, "./plugins/scraper/local/cmd/musicfs-plugin-scraper-local")
	cmd.Dir = testkit.RepositoryRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build scraper: %v %s", err, out)
	}
	worker, err := os.ReadFile(workerPath)
	if err != nil {
		t.Fatal(err)
	}
	host, err := scraperlocal.NewHost(scraperlocal.Config{WorkerPath: workerPath, WorkerSHA256: fmt.Sprintf("%x", sha256.Sum256(worker)), WorkDir: dir, InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	runtimes := []enrichmentworker.Runtime{{ID: "d248bf34-1780-59d3-a557-9cf5d889e977", Name: "Local metadata", Owner: "admin", Host: host}, {ID: "disabled", Name: "Offline scraper", Owner: "admin"}}
	service, err := enrichmentworker.New(store, lib, content, runtimes, enrichmentworker.Config{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	return fixture{service: service, store: store, content: content, lib: lib, runtimes: runtimes, tracks: page.Items, root: root, mediaRoot: mediaRoot}
}

func (f fixture) good() library.Track {
	for _, t := range f.tracks {
		if t.DisplayName == "good.mp3" {
			return t
		}
	}
	panic("fixture track unavailable")
}
func request(ids ...string) enrichment.JobRequest {
	return enrichment.JobRequest{SourceTrackIDs: ids, Fields: []string{"lyrics", "artwork"}, Languages: []string{}, InstanceIDs: []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}}
}
func finished(t *testing.T, f fixture, id string) enrichment.Job {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		j, err := f.service.Get(context.Background(), "admin", id)
		if err != nil {
			t.Fatal(err)
		}
		if j.State != "queued" && j.State != "running" {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("metadata task did not finish")
	return enrichment.Job{}
}
