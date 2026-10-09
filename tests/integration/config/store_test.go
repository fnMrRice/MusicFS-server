package config_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

const configuration = `# Keep this header.
configVersion: 1
listen: '127.0.0.1:8080' # Keep this comment.
allowLoopbackHttp: true
allowedOrigins: ['http://127.0.0.1:8080']
plugins:
  - pluginId: musicfs.ncm
    executable: './worker with spaces'
    enabled: true # Keep the plugin comment.
    instances:
      - instanceId: ncm-default
        name: '中文 实例'
        role: audio
        enabled: true # Keep the instance comment.
        config: {}
`

func prepare(t *testing.T) (string, *sqlite.Store) {
	t.Helper()
	t.Setenv("MUSICFS_ADMIN_TOKEN", "test-secret-that-must-never-be-persisted")
	dir := t.TempDir()
	path := filepath.Join(dir, "配置 file.yaml")
	if err := os.WriteFile(path, []byte(configuration), 0600); err != nil {
		t.Fatal(err)
	}
	journal, err := sqlite.Open(context.Background(), filepath.Join(dir, "state", "operations.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { journal.Close() })
	return path, journal
}

func open(t *testing.T, path string, journal config.Journal) *config.Store {
	t.Helper()
	s, err := config.OpenStore(context.Background(), path, nil, journal)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func change(s *config.Store, key string) config.Change {
	return config.Change{ExpectedRevision: s.Snapshot().Revision, IdempotencyKey: key, PluginID: "musicfs.ncm", Enabled: false}
}

func assertCode(t *testing.T, err error, code string) {
	t.Helper()
	var known *domain.Error
	if !errors.As(err, &known) || known.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestStorePreservesCommentsAndNeverWritesEffectiveOverrides(t *testing.T) {
	path, journal := prepare(t)
	t.Setenv("MUSICFS_LISTEN", "127.0.0.1:9900")
	s, err := config.OpenStore(context.Background(), path, map[string]string{"WEB_ROOT": "web-artifact"}, journal)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	initial := s.Snapshot()
	if !initial.Writable || initial.EnabledSource != "yaml" || initial.Config.AdminToken != "" || initial.Config.Listen != "127.0.0.1:9900" {
		t.Fatalf("invalid snapshot: %+v", initial)
	}
	if err := s.MarkApplied(ctx, initial.Revision); err != nil {
		t.Fatal(err)
	}
	request := change(s, "disable-plugin-1")
	op, err := s.SetEnabled(ctx, request)
	if err != nil || op.Status != "saved" || op.NextRevision == initial.Revision {
		t.Fatalf("save failed: %+v %v", op, err)
	}
	view := s.Snapshot()
	if !view.RestartRequired || view.AppliedRevision != initial.Revision || view.Config.Plugins[0].Enabled || !view.Config.Plugins[0].Instances[0].Enabled {
		t.Fatalf("save changed applied state or child flag: %+v", view)
	}
	view.Config.Plugins[0].Instances[0].Name = "mutated"
	if s.Snapshot().Config.Plugins[0].Instances[0].Name == "mutated" {
		t.Fatal("snapshot aliases internal configuration")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"# Keep this header.", "# Keep this comment.", "# Keep the plugin comment.", "# Keep the instance comment.", "中文 实例", "127.0.0.1:8080", "./worker with spaces"} {
		if !strings.Contains(string(raw), expected) {
			t.Fatalf("unrelated YAML content was lost: %s", expected)
		}
	}
	for _, forbidden := range []string{"test-secret", "9900", "web-artifact", "maxCacheBytes", "dataRoot"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("effective configuration leaked to YAML: %s", forbidden)
		}
	}
	replay, err := s.SetEnabled(ctx, request)
	if err != nil || replay.ID != op.ID || replay.NextRevision != op.NextRevision {
		t.Fatal("idempotent replay did not return the original operation", err)
	}
	request.Enabled = true
	_, err = s.SetEnabled(ctx, request)
	assertCode(t, err, "CONFLICT")
	request.IdempotencyKey = "another-request"
	_, err = s.SetEnabled(ctx, request)
	assertCode(t, err, "PRECONDITION_FAILED")
	if err = s.MarkApplied(ctx, op.NextRevision); err != nil || s.Snapshot().RestartRequired {
		t.Fatal("applied revision was not recorded", err)
	}
	replay, err = s.SetEnabled(ctx, changeWithRevision(request, op.NextRevision))
	if err != nil || replay.Status != "saved" {
		t.Fatal("second valid save failed", err)
	}
}

func changeWithRevision(request config.Change, revision string) config.Change {
	request.ExpectedRevision = revision
	return request
}

func TestExternalEditsAndFailedReloadKeepLoadedConfiguration(t *testing.T) {
	path, journal := prepare(t)
	s := open(t, path, journal)
	before := s.Snapshot()
	request := change(s, "external-edit-request")
	if err := os.WriteFile(path, []byte(configuration+"unknown: private-value\n"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.SetEnabled(context.Background(), request)
	assertCode(t, err, "CONFIG_NOT_APPLIED")
	if err := s.Reload(context.Background()); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("invalid reload accepted or leaked input", err)
	}
	if got := s.Snapshot(); got.Revision != before.Revision || !got.Config.Plugins[0].Enabled {
		t.Fatal("failed reload partially changed configuration")
	}
	updated := strings.Replace(configuration, "enabled: true # Keep the instance", "enabled: false # Keep the instance", 1)
	if err := os.WriteFile(path, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Config.Plugins[0].Instances[0].Enabled {
		t.Fatal("valid reload did not load the desired state")
	}
}

func TestReadOnlyAndMissingJournalCannotWrite(t *testing.T) {
	path, journal := prepare(t)
	s := open(t, path, nil)
	_, err := s.SetEnabled(context.Background(), change(s, "no-journal-request"))
	assertCode(t, err, "CONFIG_READ_ONLY")
	s = open(t, path, journal)
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(path, 0600) })
	if s.Snapshot().Writable {
		t.Fatal("read-only configuration reported writable")
	}
	_, err = s.SetEnabled(context.Background(), change(s, "read-only-request"))
	assertCode(t, err, "CONFIG_READ_ONLY")
	raw, _ := os.ReadFile(path)
	if !bytes.Equal(raw, []byte(configuration)) {
		t.Fatal("read-only configuration changed")
	}
}

func TestConcurrentRevisionUpdatesAndRestartPersistence(t *testing.T) {
	path, journal := prepare(t)
	s := open(t, path, journal)
	first := change(s, "concurrent-request-1")
	second := first
	second.IdempotencyKey = "concurrent-request-2"
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, request := range []config.Change{first, second} {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := s.SetEnabled(context.Background(), request)
			results <- err
		}()
	}
	wait.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			assertCode(t, err, "PRECONDITION_FAILED")
		}
	}
	if successes != 1 {
		t.Fatalf("expected one revision winner, got %d", successes)
	}
	restarted := open(t, path, journal)
	if restarted.Snapshot().Config.Plugins[0].Enabled || restarted.Snapshot().Revision != s.Snapshot().Revision {
		t.Fatal("saved intent did not survive reopening")
	}
}
