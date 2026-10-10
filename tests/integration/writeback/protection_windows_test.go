package writeback_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

type observingJournal struct {
	writeback.Journal
	observe func(writeback.Record)
}

func (j observingJournal) Save(ctx context.Context, from string, r writeback.Record) error {
	if err := j.Journal.Save(ctx, from, r); err != nil {
		return err
	}
	j.observe(r)
	return nil
}

func TestNTFSWritebackProtectsOriginalAndDoesNotClobberGap(t *testing.T) {
	root, original, p, r, h, j := setup(t)
	filename := filepath.Join(root, r.Target.RelativePath)
	external := append(fixture(t), []byte("external creator")...)
	written, displaced := false, false
	observer := observingJournal{Journal: j, observe: func(record writeback.Record) {
		switch record.Stage {
		case "written":
			written = true
			f, err := os.OpenFile(filename, os.O_WRONLY, 0)
			if err == nil {
				f.Close()
				t.Error("write handle opened during protected edit")
			}
			if err := os.Rename(filename, filename+".foreign"); err == nil {
				t.Error("original was externally renamed")
			}
		case "displaced":
			displaced = true
			if err := os.WriteFile(filename, external, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}}
	if _, err := service(t, p, observer, h).Apply(context.Background(), "admin", r); err == nil {
		t.Fatal("external gap occupant overwritten")
	}
	if !written || !displaced {
		t.Fatal("protection assertions not reached")
	}
	got, err := os.ReadFile(filename)
	if err != nil || !bytes.Equal(got, external) {
		t.Fatal("external content lost")
	}
	backup, err := os.ReadFile(filepath.Join(root, ".musicfs-writeback-"+r.ID+".bak"))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("original backup lost")
	}
	if _, err := service(t, p, j, h).Recover(context.Background(), "admin", r.ID); err == nil {
		t.Fatal("recovery accepted foreign occupant")
	}
	pending, err := j.Pending(context.Background(), "", 128)
	if err != nil || len(pending) != 1 || pending[0].Stage != "blocked" {
		t.Fatal("blocked source lost its restart recovery barrier", err)
	}
}

func TestNTFSWritebackCancellationAndExistingWriter(t *testing.T) {
	for _, scenario := range []string{"cancel", "writer", "size", "moved-root"} {
		t.Run(scenario, func(t *testing.T) {
			root, original, p, r, h, j := setup(t)
			filename := filepath.Join(root, r.Target.RelativePath)
			ctx := context.Background()
			switch scenario {
			case "cancel":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "writer":
				f, e := os.OpenFile(filename, os.O_WRONLY, 0)
				if e != nil {
					t.Fatal(e)
				}
				defer f.Close()
			case "size":
				p.MaxFileBytes = 64
			case "moved-root":
				p.Roots[0].Identity = "another-root"
			}
			if _, err := service(t, p, j, h).Apply(ctx, "admin", r); err == nil {
				t.Fatal("unsafe operation accepted")
			}
			got, e := os.ReadFile(filename)
			if e != nil || !bytes.Equal(got, original) {
				t.Fatal("failed request mutated original")
			}
		})
	}
}

type exitJournal struct {
	writeback.Journal
	stage string
}

func (j exitJournal) Save(ctx context.Context, from string, r writeback.Record) error {
	if err := j.Journal.Save(ctx, from, r); err != nil {
		return err
	}
	if r.Stage == j.stage {
		os.Exit(23)
	}
	return nil
}
func TestWritebackCrashHelper(t *testing.T) {
	directory := os.Getenv("MUSICFS_WRITEBACK_TEST_CRASH")
	if directory == "" {
		return
	}
	raw, err := os.ReadFile(filepath.Join(directory, "request.json"))
	if err != nil {
		t.Fatal(err)
	}
	var r writeback.Request
	if err = json.Unmarshal(raw, &r); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(directory, "music")
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	p := writeback.Policy{Enabled: true, Roots: []writeback.Root{{Owner: "admin", InstanceID: "local", RootID: "music", Path: root, Identity: a.Roots()[0].ExpectedIdentity, Fields: []string{"title", "lyrics", "artwork"}}}}
	j, err := writeback.OpenJournal(filepath.Join(directory, "journal"))
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	h := &host{target: r.Target, revision: r.SelectionRevision}
	_, err = service(t, p, exitJournal{j, os.Getenv("MUSICFS_WRITEBACK_TEST_STAGE")}, h).Apply(context.Background(), "admin", r)
	t.Fatalf("crash boundary not reached: %v", err)
}
func TestNTFSWritebackRealProcessExitRecovery(t *testing.T) {
	for _, stage := range []string{"written", "displaced", "installed"} {
		t.Run(stage, func(t *testing.T) {
			directory := t.TempDir()
			root := filepath.Join(directory, "music")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			original := fixture(t)
			filename := "track.flac"
			if err := os.WriteFile(filepath.Join(root, filename), original, 0600); err != nil {
				t.Fatal(err)
			}
			p, r, h := requestFor(t, root, filename)
			raw, _ := json.Marshal(r)
			if err := os.WriteFile(filepath.Join(directory, "request.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			program, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(program, "-test.run=^TestWritebackCrashHelper$")
			cmd.Env = append(os.Environ(), "MUSICFS_WRITEBACK_TEST_CRASH="+directory, "MUSICFS_WRITEBACK_TEST_STAGE="+stage)
			out, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("helper did not exit at boundary: %v %s", err, out)
			}
			j, err := writeback.OpenJournal(filepath.Join(directory, "journal"))
			if err != nil {
				t.Fatal(err)
			}
			defer j.Close()
			result, err := service(t, p, j, h).Recover(context.Background(), "admin", r.ID)
			if err != nil {
				t.Fatal(err)
			}
			backup, err := os.ReadFile(filepath.Join(root, result.BackupRelativePath))
			if err != nil || !bytes.Equal(backup, original) {
				t.Fatal("restart lost original")
			}
			if h.commits != 1 {
				t.Fatal("restart did not publish exactly once")
			}
		})
	}
}
