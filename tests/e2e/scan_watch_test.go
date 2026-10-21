package e2e_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func TestYAMLWatchRealEventsScopeRestartAndDefaultOff(t *testing.T) {
	worker := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	write := func(name string) {
		t.Helper()
		path := filepath.Join(music, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("original.wav")
	cfg, base := yamlServiceConfig(t)
	depth, rules := 1, []string{"private"}
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: worker, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Name: "Local", Enabled: true, ImportDefaults: &pluginproto.ImportOverrides{MaxDepth: &depth, ExcludePatterns: &rules}, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "root", Name: "Music", Path: music}}}}}}}
	t.Run("default-off", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		// The normal startup interval exceeds the enabled test's debounce and scan duration.
		time.Sleep(1200 * time.Millisecond)
		page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
		if len(page.Items) != 0 {
			t.Fatal("default-off watch imported files")
		}
	})
	watch := config.DefaultWatch()
	watch.Enabled = true
	watch.DebounceMs = 100
	watch.MaxDelayMs = 300
	watch.FullRescanSeconds = 30
	cfg.Plugins[0].Instances[0].Config.Watch = &watch
	originalID := ""
	t.Run("native-notifications", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		items := waitWatchedTracks(t, c, func(items map[string]library.Track) bool {
			return len(items) == 1 && items["original.wav"].Availability == "available"
		})
		originalID = items["original.wav"].ID
		write("private/hidden.wav")
		write("new/deep/too-deep.wav")
		write("new/child.wav")
		items = waitWatchedTracks(t, c, func(items map[string]library.Track) bool {
			return len(items) == 2 && items["child.wav"].Availability == "available"
		})
		if _, ok := items["hidden.wav"]; ok {
			t.Fatal("watch bypassed exclusions")
		}
		if _, ok := items["too-deep.wav"]; ok {
			t.Fatal("watch bypassed depth")
		}
		// The new directory is now registered; later writes need no parent event.
		write("new/later.wav")
		waitWatchedTracks(t, c, func(items map[string]library.Track) bool {
			return len(items) == 3 && items["later.wav"].Availability == "available"
		})
		if err := os.Rename(filepath.Join(music, "original.wav"), filepath.Join(music, "renamed.wav")); err != nil {
			t.Fatal(err)
		}
		items = waitWatchedTracks(t, c, func(items map[string]library.Track) bool { return items["renamed.wav"].Availability == "available" })
		if items["renamed.wav"].ID != originalID {
			t.Fatal("watch rename changed source identity")
		}
		if err := os.Remove(filepath.Join(music, "new", "child.wav")); err != nil {
			t.Fatal(err)
		}
		waitWatchedTracks(t, c, func(items map[string]library.Track) bool { return items["child.wav"].Availability == "missing" })
	})
	write("offline-added.wav")
	t.Run("startup-reconciles-offline-changes", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		items := waitWatchedTracks(t, c, func(items map[string]library.Track) bool {
			return items["offline-added.wav"].Availability == "available"
		})
		if items["renamed.wav"].ID != originalID || items["child.wav"].Availability != "missing" {
			t.Fatal("restart lost stable identity or reconciliation")
		}
	})
	for _, name := range []string{"renamed.wav", "new/later.wav", "private/hidden.wav", "new/deep/too-deep.wav", "offline-added.wav"} {
		actual, err := os.ReadFile(filepath.Join(music, filepath.FromSlash(name)))
		if err != nil || !bytes.Equal(actual, payload) {
			t.Fatal("watch changed original file bytes", err)
		}
	}
}

func waitWatchedTracks(t *testing.T, c *client, ready func(map[string]library.Track) bool) map[string]library.Track {
	t.Helper()
	var items map[string]library.Track
	for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
		page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
		items = map[string]library.Track{}
		for _, item := range page.Items {
			items[item.DisplayName] = item
		}
		if ready(items) {
			return items
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("watch did not reconcile expected source set: %+v", items)
	return nil
}
