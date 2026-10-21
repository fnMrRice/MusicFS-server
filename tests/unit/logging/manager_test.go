package logging_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

const pluginID = "musicfs.plugin.source.localfiles"

func newManager(t *testing.T, dir string, console io.Writer) *logging.Manager {
	t.Helper()
	m, err := logging.New(logging.Options{Directory: dir, Console: console})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	return m
}

func contents(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestLevelsRoutingAndExistingLoggers(t *testing.T) {
	dir := t.TempDir()
	var console bytes.Buffer
	m := newManager(t, dir, &console)
	if err := m.RegisterPlugins(pluginID, "musicfs.scraper.local"); err != nil {
		t.Fatal(err)
	}
	log := m.Logger()
	plugin := log.With("pluginId", pluginID, "instanceId", "instance-a")
	log.Debug("hidden server debug")
	log.Info("server visible", "token", "never-store-this")
	plugin.Debug("hidden plugin debug")
	if err := m.Configure(logging.Settings{Level: "warn", Plugins: map[string]string{pluginID: "trace"}}); err != nil {
		t.Fatal(err)
	}
	plugin.Log(context.Background(), logging.LevelTrace, "plugin trace visible")
	log.Debug("record plugin visible", "pluginId", pluginID)
	log.Info("hidden server info")
	log.Warn("server warning")
	if err := m.Configure(logging.Settings{Level: "debug", Plugins: map[string]string{pluginID: "bogus"}}); err == nil {
		t.Fatal("invalid update accepted")
	}
	if m.Settings().Level != "warn" {
		t.Fatal("invalid update changed settings")
	}
	if err := m.Configure(logging.Settings{Level: "info"}); err != nil {
		t.Fatal(err)
	}
	plugin.Debug("hidden after reset")
	plugin.WithGroup("detail").Info("grouped plugin event", "count", 2)
	server := contents(t, filepath.Join(dir, "server.log"))
	file := contents(t, filepath.Join(dir, "plugins", pluginID+".log"))
	for _, text := range []string{server, file, console.String()} {
		if strings.Contains(text, "hidden") || strings.Contains(text, "never-store-this") {
			t.Fatalf("filtered or secret data was logged: %s", text)
		}
	}
	if strings.Contains(server, "plugin trace") || !strings.Contains(file, `"level":"TRACE"`) || !strings.Contains(file, "record plugin visible") || !strings.Contains(file, `"detail":{"count":2}`) {
		t.Fatal("plugin routing or trace formatting failed")
	}
	if !strings.Contains(console.String(), "server visible") || !strings.Contains(console.String(), "plugin trace visible") {
		t.Fatal("console did not receive both streams")
	}
	if data := contents(t, filepath.Join(dir, "plugins", "musicfs.scraper.local.log")); data != "" {
		t.Fatal("inactive plugin contains other plugin logs")
	}
}

func TestConcurrentLoggingAndConfiguration(t *testing.T) {
	dir := t.TempDir()
	m := newManager(t, dir, io.Discard)
	log := m.Logger().With("pluginId", pluginID)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Go(func() {
			for n := 0; n < 100; n++ {
				log.Info("concurrent record", "n", n)
			}
		})
	}
	group.Go(func() {
		for n := 0; n < 100; n++ {
			level := "info"
			if n%2 == 0 {
				level = "debug"
			}
			if err := m.Configure(logging.Settings{Level: level}); err != nil {
				t.Error(err)
			}
		}
	})
	group.Wait()
	lines := strings.Split(strings.TrimSpace(contents(t, filepath.Join(dir, "plugins", pluginID+".log"))), "\n")
	if len(lines) != 800 {
		t.Fatalf("lost records: %d", len(lines))
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatal("interleaved JSON records")
		}
	}
}

func waitArchive(t *testing.T, pattern string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		paths, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range paths {
			f, err := os.Open(path)
			if err != nil {
				continue
			}
			reader, err := gzip.NewReader(f)
			if err == nil {
				data, readErr := io.ReadAll(reader)
				reader.Close()
				f.Close()
				if readErr == nil {
					return string(data)
				}
			} else {
				f.Close()
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("compressed archive was not completed")
	return ""
}

func TestRotationCompressionAndThirtyDayRetention(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().UTC().Add(-31 * 24 * time.Hour).Format("2006-01-02T15-04-05.000")
	expired := filepath.Join(dir, "server-"+old+".log.gz")
	unrelated := filepath.Join(dir, "unrelated-"+old+".log.gz")
	for _, path := range []string{expired, unrelated} {
		if err := os.WriteFile(path, []byte("marker"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := newManager(t, dir, io.Discard)
	if _, err := os.Stat(expired); !os.IsNotExist(err) {
		t.Fatal("expired archive retained")
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatal("unrelated file was removed")
	}
	m.Logger().Info("archived server record")
	m.Logger().Info("archived plugin record", "pluginId", pluginID)
	if err := m.Rotate(); err != nil {
		t.Fatal(err)
	}
	m.Logger().Info("current server record")
	if text := waitArchive(t, filepath.Join(dir, "server-*.gz")); !strings.Contains(text, "archived server record") || strings.Contains(text, "current server record") {
		t.Fatal("archive content is wrong")
	}
	if text := waitArchive(t, filepath.Join(dir, "plugins", pluginID+"-*.gz")); !strings.Contains(text, "archived plugin record") {
		t.Fatal("plugin archive content is wrong")
	}
	if text := contents(t, filepath.Join(dir, "server.log")); strings.Contains(text, "archived server record") || !strings.Contains(text, "current server record") {
		t.Fatal("active file was not rotated")
	}
}

func TestPreviousDayFileRotatesAtStartup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	if err := os.WriteFile(path, []byte("yesterday\n"), 0600); err != nil {
		t.Fatal(err)
	}
	yesterday := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(path, yesterday, yesterday); err != nil {
		t.Fatal(err)
	}
	newManager(t, dir, io.Discard).Logger().Info("today")
	if text := waitArchive(t, filepath.Join(dir, "server-*.gz")); text != "yesterday\n" {
		t.Fatal("previous day was not archived")
	}
}

func TestPluginIdentifiersCannotEscapeDirectory(t *testing.T) {
	m := newManager(t, t.TempDir(), io.Discard)
	for _, id := range []string{"../escape", "..", "CON", "nul.ext", "plugin.", "a/b", "a\\b", "a:stream"} {
		if logging.ValidPluginID(id) || m.RegisterPlugins(id) == nil {
			t.Fatalf("unsafe identifier accepted: %q", id)
		}
	}
}

func TestStartupDoesNotRenewExpiredActiveLogs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	if err := os.WriteFile(path, []byte("expired record\n"), 0600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-31 * 24 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	m := newManager(t, dir, io.Discard)
	m.Logger().Info("new record")
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(contents(t, path), "expired") {
		t.Fatal("expired active log was retained")
	}
	archives, err := filepath.Glob(filepath.Join(dir, "server-*.gz"))
	if err != nil || len(archives) != 0 {
		t.Fatal("expired active log was given a new retention period")
	}
}

func TestSizeRotationFollowedByManualRotationPreservesBothArchives(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	oldSize := int64(100<<20) - 256
	if err := file.Truncate(oldSize); err != nil {
		file.Close()
		t.Fatal(err)
	}
	file.Close()
	m := newManager(t, dir, io.Discard)
	m.Logger().Info(strings.Repeat("x", 512))
	if err := m.Rotate(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(dir, "server-*.log.gz"))
	if err != nil || len(paths) != 2 {
		t.Fatalf("rotation lost an archive: %v (%v)", paths, err)
	}
	var total int64
	for _, path := range paths {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			t.Fatal(err)
		}
		n, err := io.Copy(io.Discard, reader)
		reader.Close()
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		total += n
	}
	if total <= oldSize+512 {
		t.Fatal("size-triggered archive contents were overwritten")
	}
}
