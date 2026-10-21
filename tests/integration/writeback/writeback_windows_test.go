package writeback_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/tags/flac"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

func setup(t *testing.T) (string, []byte, writeback.Policy, writeback.Request, *host, *writeback.FileJournal) {
	t.Helper()
	root := t.TempDir()
	relative := "中文 空格曲目.flac"
	original := fixture(t)
	if err := os.WriteFile(filepath.Join(root, relative), original, 0600); err != nil {
		t.Fatal(err)
	}
	policy, request, h := requestFor(t, root, relative)
	j, err := writeback.OpenJournal(filepath.Join(t.TempDir(), "journal"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { j.Close() })
	return root, original, policy, request, h, j
}
func service(t *testing.T, p writeback.Policy, j writeback.Journal, h *host) *writeback.Service {
	t.Helper()
	s, e := writeback.New(p, j, h, flac.New())
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestNTFSWritebackAndBackup(t *testing.T) {
	root, original, p, r, h, j := setup(t)
	result, err := service(t, p, j, h).Apply(context.Background(), "admin", r)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Old.Identity == result.New.Identity || h.commits != 1 || result.SourceTrackID != "stable-track" {
		t.Fatalf("invalid result: %+v", result)
	}
	backup, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(result.BackupRelativePath)))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatalf("backup: %v", err)
	}
	edited, err := os.ReadFile(filepath.Join(root, r.Target.RelativePath))
	if err != nil {
		t.Fatal(err)
	}
	_, before := parse(t, original)
	_, after := parse(t, edited)
	if !bytes.Equal(before, after) {
		t.Fatal("audio changed")
	}
	a, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "music", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := a.Inspect("music", r.Target.RelativePath)
	if err != nil || entry.ObservedVersion != result.New.Version {
		t.Fatalf("published version drifted: %v %s != %s", err, entry.ObservedVersion, result.New.Version)
	}
	second, err := service(t, p, j, h).Apply(context.Background(), "admin", r)
	if err != nil || second != result || h.commits != 1 {
		t.Fatalf("replay: %+v %v", second, err)
	}
	decoder := "D:/Software/MultiMedia/ffmpeg-6.1.1-full_build-shared/bin/ffmpeg.exe"
	if _, err := os.Stat(decoder); err != nil {
		t.Log("FFmpeg unavailable; byte-level payload assertions passed")
		return
	}
	decode := func(name string) []byte {
		cmd := exec.Command(decoder, "-v", "error", "-i", name, "-map", "0:a:0", "-f", "s16le", "-")
		out, e := cmd.Output()
		if e != nil {
			t.Fatal(e)
		}
		return out
	}
	if !bytes.Equal(decode(filepath.Join(root, result.BackupRelativePath)), decode(filepath.Join(root, r.Target.RelativePath))) {
		t.Fatal("decoded audio differs")
	}
}
func TestNTFSWritebackDefaultAndAuthorization(t *testing.T) {
	for _, scenario := range []string{"disabled", "root", "field", "revision", "busy", "readonly", "hardlink", "stream"} {
		t.Run(scenario, func(t *testing.T) {
			root, original, p, r, h, j := setup(t)
			filename := filepath.Join(root, r.Target.RelativePath)
			switch scenario {
			case "disabled":
				p.Enabled = false
			case "root":
				p.Roots[0].Owner = "another"
			case "field":
				p.Roots[0].Fields = []string{"album"}
			case "revision":
				r.SelectionRevision++
			case "busy":
				h.busy = true
			case "readonly":
				if e := os.Chmod(filename, 0400); e != nil {
					t.Fatal(e)
				}
				t.Cleanup(func() { os.Chmod(filename, 0600) })
			case "hardlink":
				if e := os.Link(filename, filepath.Join(root, "alias.flac")); e != nil {
					t.Fatal(e)
				}
			case "stream":
				if e := os.WriteFile(filename+":private", []byte("must survive"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if _, err := service(t, p, j, h).Apply(context.Background(), "admin", r); err == nil {
				t.Fatal("unsafe edit accepted")
			}
			after, e := os.ReadFile(filename)
			if e != nil || !bytes.Equal(after, original) || h.commits != 0 {
				t.Fatal("denied edit changed source")
			}
		})
	}
}
func TestNTFSWritebackCrashRecovery(t *testing.T) {
	for _, stage := range []string{"written", "displaced", "installed", "committed"} {
		for _, after := range []bool{false, true} {
			name := stage + "-before"
			if after {
				name = stage + "-after"
			}
			t.Run(name, func(t *testing.T) {
				root, original, p, r, h, j := setup(t)
				fault := &failingJournal{Journal: j, stage: stage, after: after}
				if _, err := service(t, p, fault, h).Apply(context.Background(), "admin", r); err == nil {
					t.Fatal("fault not reached")
				}
				result, err := service(t, p, j, h).Recover(context.Background(), "admin", r.ID)
				if stage == "written" && !after {
					if err == nil {
						t.Fatal("partial stage not aborted")
					}
					b, e := os.ReadFile(filepath.Join(root, r.Target.RelativePath))
					if e != nil || !bytes.Equal(b, original) {
						t.Fatal("prepared recovery changed original")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !result.Changed || h.commits != 1 {
					t.Fatalf("bad recovery: %+v commits=%d", result, h.commits)
				}
				backup, e := os.ReadFile(filepath.Join(root, result.BackupRelativePath))
				if e != nil || !bytes.Equal(backup, original) {
					t.Fatal("recovery lost backup")
				}
			})
		}
	}
}
func TestNTFSRecoveryNeverOverwritesExternalChanges(t *testing.T) {
	for _, stage := range []string{"written", "displaced", "installed"} {
		t.Run(stage, func(t *testing.T) {
			root, _, p, r, h, j := setup(t)
			fault := &failingJournal{Journal: j, stage: stage, after: true}
			if _, err := service(t, p, fault, h).Apply(context.Background(), "admin", r); err == nil {
				t.Fatal("fault not reached")
			}
			external := append(fixture(t), []byte("external edit")...)
			filename := filepath.Join(root, r.Target.RelativePath)
			if err := os.WriteFile(filename, external, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := service(t, p, j, h).Recover(context.Background(), "admin", r.ID); err == nil {
				t.Fatal("external change accepted")
			}
			current, err := os.ReadFile(filename)
			if err != nil || !bytes.Equal(current, external) {
				t.Fatal("external file overwritten")
			}
			if h.commits != 0 {
				t.Fatal("external content was published")
			}
		})
	}
}
func TestNTFSWritebackDatabaseFailureKeepsRecoverableBackup(t *testing.T) {
	root, original, p, r, h, j := setup(t)
	h.failCommit = true
	if _, err := service(t, p, j, h).Apply(context.Background(), "admin", r); err == nil {
		t.Fatal("database failure not returned")
	}
	record, err := j.Load(context.Background(), r.ID)
	if err != nil || record.Stage != "installed" {
		t.Fatalf("lost intent: %v %+v", err, record)
	}
	if _, err = os.Stat(filepath.Join(root, r.Target.RelativePath)); err != nil {
		t.Fatal(err)
	}
	h.failCommit = false
	result, err := service(t, p, j, h).Recover(context.Background(), "admin", r.ID)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(filepath.Join(root, result.BackupRelativePath))
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("backup lost")
	}
	if _, err = service(t, p, j, h).Recover(context.Background(), "other", r.ID); err == nil || errors.Is(err, context.Canceled) {
		t.Fatal("owner bypass")
	}
}
