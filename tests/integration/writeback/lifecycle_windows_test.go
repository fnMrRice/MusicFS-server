package writeback_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

func TestNTFSWritebackConcurrentReplay(t *testing.T) {
	_, _, p, r, h, j := setup(t)
	s := service(t, p, j, h)
	var wg sync.WaitGroup
	errors := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := s.Apply(context.Background(), "admin", r); errors <- err }()
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	if h.commits != 1 {
		t.Fatal("concurrent replay committed more than once")
	}
	pending, err := j.Pending(context.Background(), "", 128)
	if err != nil || len(pending) != 0 {
		t.Fatalf("terminal intent remained pending: %v %+v", err, pending)
	}
	r.Patch.Comments["title"] = []string{"Different request"}
	if _, err := s.Apply(context.Background(), "admin", r); err == nil {
		t.Fatal("job ID accepted changed payload")
	}
}

type createJournal struct {
	writeback.Journal
	after func(writeback.Record)
}

func (j createJournal) Create(ctx context.Context, r writeback.Record) error {
	if err := j.Journal.Create(ctx, r); err != nil {
		return err
	}
	j.after(r)
	return nil
}
func TestNTFSStagingFailureAndCancelLeaveOriginal(t *testing.T) {
	for _, scenario := range []string{"create-failure", "cancel-before-rename"} {
		t.Run(scenario, func(t *testing.T) {
			root, original, p, r, h, j := setup(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var journal writeback.Journal = j
			if scenario == "create-failure" {
				journal = createJournal{j, func(record writeback.Record) {
					if err := os.Mkdir(filepath.Join(root, ".musicfs-writeback-"+record.ID+".new"), 0700); err != nil {
						t.Fatal(err)
					}
				}}
			} else {
				journal = observingJournal{j, func(record writeback.Record) {
					if record.Stage == "written" {
						cancel()
					}
				}}
			}
			if _, err := service(t, p, journal, h).Apply(ctx, "admin", r); err == nil {
				t.Fatal("failure ignored")
			}
			current, err := os.ReadFile(filepath.Join(root, r.Target.RelativePath))
			if err != nil || !bytes.Equal(current, original) {
				t.Fatal("failure modified original")
			}
			pending, err := j.Pending(context.Background(), "", 1)
			if err != nil || len(pending) != 1 {
				t.Fatalf("lost persistent intent: %v", err)
			}
			if _, err := j.Pending(context.Background(), "", 129); err == nil {
				t.Fatal("unbounded recovery page accepted")
			}
		})
	}
}

func TestNTFSNoopDoesNotReplaceOriginal(t *testing.T) {
	root, original, p, r, h, j := setup(t)
	blocks, audio := parse(t, original)
	filtered := []block{}
	for _, b := range blocks {
		if b.kind != 4 {
			filtered = append(filtered, b)
		}
	}
	filtered = append(filtered, block{4, comments("encoder", "TITLE=Keep", "UNSYNCEDLYRICS=Keep lyrics")})
	original = assemble(filtered, audio)
	if err := os.WriteFile(filepath.Join(root, r.Target.RelativePath), original, 0600); err != nil {
		t.Fatal(err)
	}
	p, r, h = requestFor(t, root, r.Target.RelativePath)
	r.Patch.Artwork = nil
	r.Patch.Overwrite = false
	result, err := service(t, p, j, h).Apply(context.Background(), "admin", r)
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed || result.BackupRelativePath != "" || result.Old != result.New {
		t.Fatalf("noop replaced source: %+v", result)
	}
	current, err := os.ReadFile(filepath.Join(root, r.Target.RelativePath))
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("fill-missing changed existing values")
	}
	if _, err := os.Stat(filepath.Join(root, ".musicfs-writeback-"+r.ID+".new")); !os.IsNotExist(err) {
		t.Fatal("unchanged staging file was retained")
	}
}
