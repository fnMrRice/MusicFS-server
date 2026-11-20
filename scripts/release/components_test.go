package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
)

func TestDefaultAndSplitReleasePrograms(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, split := range []bool{false, true} {
		b := builder{splitPlugins: split}
		want := []string{"musicfs", "musicfs-plugins"}
		if split {
			want = append(want, "musicfs-plugin-storage-sqlite", "musicfs-plugin-dsp-gain",
				"musicfs-plugin-netease", "musicfs-plugin-qqmusic", "musicfs-plugin-applemusic", "musicfs-plugin-bilibili", "musicfs-plugin-online")
		}
		if !slices.Equal(b.programNames(), want) {
			t.Fatalf("split=%v program catalog = %v, want %v", split, b.programNames(), want)
		}
		registered := make(map[string]bool)
		for _, item := range components(split) {
			if !slices.Contains(b.programNames(), item.Program) {
				t.Fatalf("component has no packaged executable: %s", item.Program)
			}
			registered[item.Program] = true
		}
		for _, program := range b.programs() {
			if !registered[program.name] {
				t.Fatalf("packaged executable lacks release metadata: %s", program.name)
			}
			if _, err := os.Stat(filepath.Join(repository, program.moduleDir, program.packagePath, "main.go")); err != nil {
				t.Fatalf("packaged executable has no entry point: %s: %v", program.name, err)
			}
		}
	}
	for _, path := range []string{"scripts/build.ps1", "scripts/build.sh"} {
		raw, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, program := range releasePrograms {
			if !strings.Contains(string(raw), program.name) {
				t.Fatalf("%s omits optional executable %s", path, program.name)
			}
		}
	}
}

func TestReleaseCatalogSeparatesBuiltinsAndOptionalProcessRoles(t *testing.T) {
	expected := []string{"local", "audio-ffmpeg", "ncm", "scraper-local", "netease", "qqmusic", "applemusic", "bilibili", "storage-sqlite", "dsp-gain"}
	raw, err := os.ReadFile(filepath.Join("..", "..", "sdk", "bundledplugin", "releases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var declared struct{ Plugins map[string]json.RawMessage }
	if err := json.Unmarshal(raw, &declared); err != nil {
		t.Fatal(err)
	}
	for _, split := range []bool{false, true} {
		roles := map[string]bool{}
		builtins := map[string]bool{}
		for _, item := range components(split) {
			if item.Execution != "" && (item.PluginID == "" || item.Version == "" || item.Version != bundledplugin.Version(item.PluginID)) {
				t.Fatalf("missing or inconsistent plugin version: %+v", item)
			}
			if item.Execution == "builtin" {
				if builtins[item.PluginID] {
					t.Fatalf("duplicate builtin plugin: %s", item.PluginID)
				}
				builtins[item.PluginID] = true
				if item.Program != "musicfs" || len(item.Args) != 0 {
					t.Fatalf("builtin plugin depends on a worker executable: %+v", item)
				}
				if item.PluginID == "musicfs.plugin.storage.sqlite" && !item.Required {
					t.Fatal("builtin SQLite must remain mandatory")
				}
			}
			if item.Program != "musicfs-plugins" {
				continue
			}
			if len(item.Args) != 2 || item.Args[0] != "--plugin" || !slices.Contains(expected, item.Args[1]) || roles[item.Args[1]] {
				t.Fatalf("unexpected or duplicated bundled role: %+v", item)
			}
			roles[item.Args[1]] = true
			switch item.Args[1] {
			case "storage-sqlite":
				if item.Required || item.PluginID != "musicfs.plugin.storage.sqlite" {
					t.Fatalf("standalone SQLite must remain an optional alternative: %+v", item)
				}
			case "dsp-gain":
				if item.Required || !slices.Contains(item.Requirements, "disabled by default") {
					t.Fatalf("DSP must remain optional: %+v", item)
				}
			}
		}
		if len(roles) != len(expected) {
			t.Fatalf("split=%v bundled roles = %d, want %d", split, len(roles), len(expected))
		}
		for id := range declared.Plugins {
			if !builtins[id] {
				t.Errorf("release omits builtin plugin %s", id)
			}
		}
	}
	t.Setenv("GOWORK", "/untrusted/go.work")
	got := targetEnvironment("windows", "amd64")
	if !slices.Contains(got, "GOWORK=off") || slices.Contains(got, "GOWORK=/untrusted/go.work") {
		t.Fatal("release must build modules independently of the caller's workspace")
	}
}
