package writebackhost_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type faultJournal struct {
	writeback.Journal
	mu           sync.Mutex
	stage        string
	after, fired bool
	entered      chan struct{}
	release      chan struct{}
}

func (j *faultJournal) Save(ctx context.Context, from string, r writeback.Record) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if r.Stage == j.stage && !j.fired {
		j.fired = true
		if j.entered != nil {
			if err := j.Journal.Save(ctx, from, r); err != nil {
				return err
			}
			close(j.entered)
			select {
			case <-j.release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if j.after {
			if err := j.Journal.Save(ctx, from, r); err != nil {
				return err
			}
		}
		return errors.New("injected host journal failure")
	}
	return j.Journal.Save(ctx, from, r)
}

func TestWritebackHostRecoversFileAndDatabaseBoundary(t *testing.T) {
	ffprobe := os.Getenv("MUSICFS_FFPROBE")
	if ffprobe == "" {
		t.Skip("MUSICFS_FFPROBE is required")
	}
	bin := filepath.Join(t.TempDir(), "local.exe")
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", bin, "./plugins/source/localfiles/cmd/musicfs-plugin-local")
	cmd.Dir = testkit.RepositoryRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(body))
	for _, scenario := range []struct {
		name, stage    string
		after, restart bool
	}{{"written-explicit", "written", true, false}, {"database-explicit", "committed", false, false}, {"written-startup", "written", true, true}, {"committed-release", "committed", true, false}} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			dir := t.TempDir()
			music := filepath.Join(dir, "music")
			if err := os.Mkdir(music, 0700); err != nil {
				t.Fatal(err)
			}
			audio, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/lossless.flac"))
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(music, "track.flac"), audio, 0600); err != nil {
				t.Fatal(err)
			}
			worker, err := pluginhost.Start(ctx, pluginhost.Config{Executable: bin, SHA256: checksum, InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", UserID: "admin", StateDir: filepath.Join(dir, "state"), Roots: []sourceaccess.RootConfig{{ID: "music", Path: music}}})
			if err != nil {
				t.Fatal(err)
			}
			defer worker.Close()
			store, err := sqlite.Open(ctx, filepath.Join(dir, "db.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			queue := jobs.New(store)
			defer queue.Close()
			runtimes := []library.Runtime{{ID: "b1548e96-3081-5c9c-8376-bf290e369d51", Owner: "admin", Worker: worker, ConfigRevision: "writeback-fixture-v1", Configuration: pluginconfig.LocalFiles(), ImportDefaults: pluginconfig.LocalFiles().ImportDefaults, RootNames: map[string]string{"music": "Music"}}}
			probe := func(ctx context.Context, in io.ReadSeeker, size int64) (library.Observation, error) {
				m, e := audioffmpeg.ProbeMetadata(ctx, ffprobe, in, size)
				return library.Observation{Title: m.Title, Artists: m.Artists, Container: m.Container}, e
			}
			lib := library.New(store, queue, runtimes, probe)
			if err = queue.Start(ctx); err != nil {
				t.Fatal(err)
			}
			track := scan(t, ctx, lib)
			files, err := mediastore.OpenFiles(filepath.Join(dir, "media"))
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			media := mediastore.NewContent(store, store, files)
			target := mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Version}
			object, err := media.Upload(ctx, "admin", mediastore.ContentMetadata{Target: target, Field: "lyrics", Language: "en", Format: "text", Origin: mediastore.Origin{Kind: "edit"}}, "text/plain", domain.NewID(), strings.NewReader("Synthetic writeback fixture lyrics\n"))
			if err != nil {
				t.Fatal(err)
			}
			selected, err := media.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: target, ObjectID: object.ID}, domain.NewID())
			if err != nil {
				t.Fatal(err)
			}
			journal, err := writeback.OpenJournal(filepath.Join(dir, "journal"))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			fault := &faultJournal{Journal: journal, stage: scenario.stage, after: scenario.after}
			if scenario.name == "committed-release" {
				fault.entered = make(chan struct{})
				fault.release = make(chan struct{})
			}
			cfg := writebackhost.Config{Policy: writeback.Policy{Enabled: true, Roots: []writeback.Root{{Owner: "admin", InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", RootID: "music", Path: music, Identity: worker.Access.Roots()[0].ExpectedIdentity, Fields: []string{"lyrics"}}}}, Probe: probe}
			service, err := writebackhost.New(store, lib, media, fault, runtimes, cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer func() { service.Close() }()
			request := writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Version, ExpectedRevision: selected.Revision, Fields: []writebackhost.Field{{Name: "lyrics", Language: "en"}}}
			key := domain.NewID()
			job, _, err := service.Submit(ctx, "admin", request, key)
			if err != nil {
				t.Fatal(err)
			}
			if fault.entered != nil {
				select {
				case <-fault.entered:
				case <-ctx.Done():
					t.Fatal("commit gate timeout")
				}
				pending, e := service.Get(ctx, "admin", job.ID)
				if e != nil || pending.Status != "running" || pending.Error != nil {
					close(fault.release)
					t.Fatalf("active finalization must remain running: %+v %v", pending, e)
				}
				close(fault.release)
				wait(t, ctx, service, job.ID)
				final, e := service.Get(ctx, "admin", job.ID)
				if e != nil || final.Status != "succeeded" {
					t.Fatalf("finalized job: %+v %v", final, e)
				}
				read, e := lib.HoldRead(ctx, "admin", "b1548e96-3081-5c9c-8376-bf290e369d51")
				if e != nil {
					t.Fatal(e)
				}
				read.Close()
				return
			}
			wait(t, ctx, service, job.ID)
			beforeRecovery, _ := service.Get(ctx, "admin", job.ID)
			t.Logf("interrupted job: %+v", beforeRecovery)
			// Both an unfinished file swap and a committed DB with an unfinished
			// journal must keep the source unavailable until verified recovery.
			if read, e := lib.HoldRead(ctx, "admin", "b1548e96-3081-5c9c-8376-bf290e369d51"); e == nil {
				read.Close()
				t.Fatal("unfinished journal lost its source barrier")
			}
			if scenario.restart {
				service.Close()
				service, err = writebackhost.New(store, lib, media, fault, runtimes, cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err = service.Start(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err = service.Recover(ctx, "admin", job.ID); err != nil {
					t.Fatal(err)
				}
				for ctx.Err() == nil {
					pending, _ := service.Get(ctx, "admin", job.ID)
					if pending.Status != "recovery" && pending.Status != "running" {
						break
					}
					time.Sleep(20 * time.Millisecond)
				}
			}
			final, err := service.Get(ctx, "admin", job.ID)
			if err != nil || final.Status != "succeeded" || final.Result == nil || !final.Result.Changed {
				t.Fatalf("recovery: %+v %v", final, err)
			}
			read, err := lib.HoldRead(ctx, "admin", "b1548e96-3081-5c9c-8376-bf290e369d51")
			if err != nil {
				t.Fatal(err)
			}
			read.Close()
			current, err := lib.Track(ctx, "admin", track.ID)
			if err != nil || current.AssetID == track.AssetID || current.Asset.FileSHA512 == nil || *current.Asset.FileSHA512 != final.Result.FileSHA512 {
				t.Fatalf("asset: %+v %v", current, err)
			}
			selection, err := media.ContentSelection(ctx, "admin", track.ID)
			if err != nil || len(selection.Bindings) != 1 || !selection.Bindings[0].Current || !selection.Bindings[0].ManualLocked || selection.Bindings[0].ObjectID != object.ID {
				t.Fatalf("selection: %+v %v", selection, err)
			}
			replayed, merged, err := service.Submit(ctx, "admin", request, key)
			if err != nil || !merged || replayed.ID != job.ID || replayed.Status != "succeeded" {
				t.Fatalf("replay: %+v %t %v", replayed, merged, err)
			}
			rescanned := scan(t, ctx, lib)
			if rescanned.ID != track.ID || rescanned.AssetID != current.AssetID {
				t.Fatalf("stable identity lost: before=%s after=%+v", track.ID, rescanned)
			}
		})
	}
}

func scan(t *testing.T, ctx context.Context, lib *library.Service) library.Track {
	t.Helper()
	j, _, err := lib.Scan(ctx, "admin", "b1548e96-3081-5c9c-8376-bf290e369d51", library.ScanInput{Mode: "full", RootIDs: []string{"music"}, SchemaRevision: pluginconfig.LocalFiles().SchemaRevision}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	for ctx.Err() == nil {
		j, err = lib.Job(ctx, "admin", j.ID)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status == "succeeded" {
			break
		}
		if j.Status != "queued" && j.Status != "running" {
			t.Fatalf("scan: %+v", j)
		}
		time.Sleep(20 * time.Millisecond)
	}
	page, err := lib.List(ctx, "admin", "b1548e96-3081-5c9c-8376-bf290e369d51", "", 10)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("list: %+v %v", page, err)
	}
	track, err := lib.Track(ctx, "admin", page.Items[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	return track
}
func wait(t *testing.T, ctx context.Context, s *writebackhost.Service, id string) {
	t.Helper()
	for ctx.Err() == nil {
		j, err := s.Get(ctx, "admin", id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "queued" && j.Status != "running" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("writeback did not finish")
}
