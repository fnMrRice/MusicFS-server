//go:build windows

package e2e_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func TestWindowsScanHiddenAndNoMediaKeepExcludedLibraryEntries(t *testing.T) {
	worker := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	for _, name := range []string{"visible.wav", ".dot.wav", "secret/windows.wav", "album/marked.wav"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, payload, 0600); err != nil {
			t.Fatal(err)
		}
	}
	secret, err := syscall.UTF16PtrFromString(filepath.Join(root, "secret"))
	if err != nil {
		t.Fatal(err)
	}
	attributes, err := syscall.GetFileAttributes(secret)
	if err != nil || syscall.SetFileAttributes(secret, attributes|syscall.FILE_ATTRIBUTE_HIDDEN) != nil {
		t.Fatal("cannot set Windows hidden directory", err)
	}
	marker := filepath.Join(root, "album", ".nomedia")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.localfiles", Executable: worker, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "root", Name: "Music", Path: root}}}}}}}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	submit := func(key string, options map[string]any) {
		t.Helper()
		request := providerScanRequest(t, c, "local")
		request["options"] = options
		accepted := albumRequest[struct {
			JobID string `json:"jobId"`
		}](t, c, "POST", "/provider-instances/local/scan-jobs", request, key, 202, "ScanAccepted")
		waitScanOptions(t, c, accepted.JobID)
	}
	list := func() map[string]library.Track {
		page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
		result := map[string]library.Track{}
		for _, track := range page.Items {
			result[track.DisplayName] = track
		}
		return result
	}
	submit("filters-default", map[string]any{})
	if items := list(); len(items) != 1 || items["visible.wav"].Availability != "available" {
		t.Fatal("new defaults imported hidden or marked media", items)
	}
	submit("filters-include-all", map[string]any{"skipHidden": false, "respectNoMedia": false})
	before := list()
	if len(before) != 4 {
		t.Fatal("filter overrides failed to import all sources", before)
	}
	for _, name := range []string{"visible.wav", ".dot.wav", "secret/windows.wav", "album/marked.wav"} {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	submit("filters-default-again", map[string]any{})
	after := list()
	if after["visible.wav"].Availability != "missing" {
		t.Fatal("visible missing track was not reconciled")
	}
	for _, name := range []string{".dot.wav", "windows.wav", "marked.wav"} {
		if after[name].ID != before[name].ID || after[name].Availability != "available" || !after[name].Asset.Current {
			t.Fatal("excluded existing source was incorrectly marked missing", name)
		}
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	submit("filters-marker-removed", map[string]any{})
	if items := list(); items["marked.wav"].Availability != "missing" || items["windows.wav"].Availability != "available" {
		t.Fatal("marker removal did not restore precisely its branch", items)
	}
	submit("filters-disabled-again", map[string]any{"skipHidden": false, "respectNoMedia": false})
	for name, item := range list() {
		if item.Availability != "missing" {
			t.Fatal("explicit filter override did not reconcile absent source", name)
		}
	}
}

func TestWindowsWatchNoMediaMarkerRemovalAndPreservedLibrary(t *testing.T) {
	worker := build(t, "musicfs-plugin-local")
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	file, marker := filepath.Join(root, "watched.wav"), filepath.Join(root, ".nomedia")
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	watch := config.DefaultWatch()
	watch.Enabled, watch.DebounceMs, watch.MaxDelayMs, watch.FullRescanSeconds = true, 100, 300, 30
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.localfiles", Executable: worker, Enabled: true, Instances: []config.Instance{{InstanceID: "local", Role: "provider", Enabled: true, Config: config.InstanceConfig{Watch: &watch, MusicRoots: []config.MusicRoot{{ID: "root", Name: "Music", Path: root}}}}}}}
	startNCMServer(t, cfg, base)
	c := login(t, base, cfg.AdminToken)
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitWatchedTracks(t, c, func(items map[string]library.Track) bool { return items["watched.wav"].Availability == "available" })
	before := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
	if err := os.WriteFile(marker, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	changed := false
	for deadline := time.Now().Add(12 * time.Second); time.Now().Before(deadline); {
		page := albumRequest[library.Page](t, c, "GET", "/source-tracks", nil, "", 200, "SourceTracks")
		if len(page.Items) != 1 || page.Items[0].Availability != "available" {
			t.Fatal("marked root lost its existing catalog entry", page)
		}
		if page.LibraryRevision != before.LibraryRevision {
			changed = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !changed {
		t.Fatal("watch did not complete marked-root scan")
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	waitWatchedTracks(t, c, func(items map[string]library.Track) bool { return items["watched.wav"].Availability == "missing" })
}
