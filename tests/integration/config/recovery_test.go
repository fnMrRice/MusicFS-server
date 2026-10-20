package config_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/config"
)

type interruptedJournal struct {
	config.Journal
	phase string
}

func (j interruptedJournal) BeginConfigOperation(ctx context.Context, op config.Operation) error {
	if j.phase == "before-intent" {
		return errors.New("journal unavailable")
	}
	if err := j.Journal.BeginConfigOperation(ctx, op); err != nil {
		return err
	}
	if j.phase == "before-replace" {
		os.Exit(71)
	}
	if j.phase == "read-only-after-intent" {
		return os.Chmod(op.ConfigPath, 0400)
	}
	return nil
}

func (j interruptedJournal) SetConfigOperationStatus(ctx context.Context, id, status string) error {
	if j.phase == "after-replace" && status == "saved" {
		os.Exit(72)
	}
	return j.Journal.SetConfigOperationStatus(ctx, id, status)
}

func TestConfigCrashHelper(t *testing.T) {
	phase := os.Getenv("MUSICFS_TEST_CONFIG_CRASH")
	if phase == "" {
		return
	}
	path := os.Getenv("MUSICFS_TEST_CONFIG_PATH")
	journal, err := sqlite.Open(context.Background(), filepath.Join(filepath.Dir(path), "state", "operations.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	s := open(t, path, interruptedJournal{Journal: journal, phase: phase})
	if _, err := s.SetEnabled(context.Background(), change(s, "crash-window-request")); err != nil {
		t.Fatal(err)
	}
	t.Fatal("expected helper to exit at the selected crash boundary")
}

func TestWriteIntentAndAtomicReplacementCrashRecovery(t *testing.T) {
	for _, phase := range []string{"before-replace", "after-replace", "superseded"} {
		t.Run(phase, func(t *testing.T) {
			path, journal := prepare(t)
			journal.Close()
			helperPhase := phase
			if phase == "superseded" {
				helperPhase = "after-replace"
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestConfigCrashHelper$")
			cmd.Env = append(os.Environ(), "MUSICFS_TEST_CONFIG_CRASH="+helperPhase, "MUSICFS_TEST_CONFIG_PATH="+path)
			output, err := cmd.CombinedOutput()
			var failure *exec.ExitError
			if !errors.As(err, &failure) || (failure.ExitCode() != 71 && failure.ExitCode() != 72) {
				t.Fatalf("unexpected crash helper exit: %v %s", err, output)
			}
			if phase == "superseded" {
				if err := os.WriteFile(path, []byte(configuration+"# A newer administrator edit.\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			beforeRecovery, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			journal, err = sqlite.Open(context.Background(), filepath.Join(filepath.Dir(path), "state", "operations.sqlite"))
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			s := open(t, path, journal)
			op, found, err := journal.FindConfigOperation(context.Background(), path, "crash-window-request")
			if err != nil || !found {
				t.Fatal("durable intent missing after process exit", err)
			}
			expected := map[string]string{"before-replace": "notSaved", "after-replace": "saved", "superseded": "superseded"}[phase]
			if op.Status != expected || s.Snapshot().Config.Plugins[0].Enabled != (phase != "after-replace") {
				t.Fatalf("wrong recovered outcome: %s, %s", phase, op.Status)
			}
			afterRecovery, _ := os.ReadFile(path)
			if !bytes.Equal(beforeRecovery, afterRecovery) {
				t.Fatal("recovery replayed intent over the authoritative YAML")
			}
		})
	}
}

func TestJournalFailureAndInstanceOnlyEdits(t *testing.T) {
	path, journal := prepare(t)
	s := open(t, path, interruptedJournal{Journal: journal, phase: "before-intent"})
	if _, err := s.SetEnabled(context.Background(), change(s, "journal-failure-request")); err == nil {
		t.Fatal("write accepted without durable intent")
	}
	raw, _ := os.ReadFile(path)
	if !bytes.Equal(raw, []byte(configuration)) {
		t.Fatal("failed intent changed YAML")
	}
	s = open(t, path, journal)
	request := change(s, "instance-only-request")
	request.InstanceID = ncmDefaultInstanceID
	if _, err := s.SetEnabled(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	view := s.Snapshot()
	if !view.Config.Plugins[0].Enabled || view.Config.Plugins[0].Instances[0].Enabled {
		t.Fatal("instance write changed parent state")
	}
	missing := strings.Replace(configuration, "    enabled: true # Keep the plugin comment.\n", "", 1)
	if err := os.WriteFile(path, []byte(missing), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	request = change(s, "insert-enabled-request")
	request.Enabled = true
	if _, err := s.SetEnabled(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if !s.Snapshot().Config.Plugins[0].Enabled {
		t.Fatal("missing enabled field was not inserted")
	}
}

func TestReplacementFailureRetainsOriginalFile(t *testing.T) {
	path, journal := prepare(t)
	s := open(t, path, interruptedJournal{Journal: journal, phase: "read-only-after-intent"})
	t.Cleanup(func() { os.Chmod(path, 0600) })
	_, err := s.SetEnabled(context.Background(), change(s, "replacement-failure-request"))
	assertCode(t, err, "CONFIG_READ_ONLY")
	raw, _ := os.ReadFile(path)
	if !bytes.Equal(raw, []byte(configuration)) {
		t.Fatal("failed replacement modified the original file")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	open(t, path, journal)
	op, found, err := journal.FindConfigOperation(context.Background(), path, "replacement-failure-request")
	if err != nil || !found || op.Status != "notSaved" {
		t.Fatal("failed replacement was reported as saved", err)
	}
}

type unavailableJournal struct {
	config.Journal
	fail bool
}

func (j *unavailableJournal) PendingConfigOperations(ctx context.Context, path string) ([]config.Operation, error) {
	if j.fail {
		return nil, errors.New("journal unavailable")
	}
	return j.Journal.PendingConfigOperations(ctx, path)
}

func TestFailedRecoveryDoesNotPublishReloadCandidate(t *testing.T) {
	path, journal := prepare(t)
	faulty := &unavailableJournal{Journal: journal}
	s := open(t, path, faulty)
	before := s.Snapshot()
	updated := strings.Replace(configuration, "enabled: true # Keep the plugin", "enabled: false # Keep the plugin", 1)
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	faulty.fail = true
	if err := s.Reload(context.Background()); err == nil {
		t.Fatal("reload succeeded with unavailable recovery journal")
	}
	if after := s.Snapshot(); after.Revision != before.Revision || !after.Config.Plugins[0].Enabled || !after.DiskChanged {
		t.Fatal("failed reload published a partial candidate")
	}
}
