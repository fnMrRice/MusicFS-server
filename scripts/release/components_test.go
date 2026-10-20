package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
				"musicfs-plugin-netease", "musicfs-plugin-qqmusic", "musicfs-plugin-applemusic", "musicfs-plugin-online")
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

func TestUnifiedCatalogHasNineRolesAndRequiredStorage(t *testing.T) {
	expected := []string{"local", "audio-ffmpeg", "ncm", "scraper-local", "netease", "qqmusic", "applemusic", "storage-sqlite", "dsp-gain"}
	for _, split := range []bool{false, true} {
		roles := map[string]bool{}
		for _, item := range components(split) {
			if item.Program != "musicfs-plugins" {
				continue
			}
			if len(item.Args) != 2 || item.Args[0] != "--plugin" || !slices.Contains(expected, item.Args[1]) || roles[item.Args[1]] {
				t.Fatalf("unexpected or duplicated bundled role: %+v", item)
			}
			roles[item.Args[1]] = true
			switch item.Args[1] {
			case "storage-sqlite":
				if !item.Required || item.PluginID != "" {
					t.Fatalf("SQLite must be mandatory and outside the toggleable plugin registry: %+v", item)
				}
			case "dsp-gain":
				if item.Required || !slices.Contains(item.Requirements, "disabled by default") {
					t.Fatalf("DSP must remain optional: %+v", item)
				}
			}
		}
		if len(roles) != 9 {
			t.Fatalf("split=%v bundled roles = %d, want 9", split, len(roles))
		}
	}
	t.Setenv("GOWORK", "/untrusted/go.work")
	got := targetEnvironment("windows", "amd64")
	if !slices.Contains(got, "GOWORK=off") || slices.Contains(got, "GOWORK=/untrusted/go.work") {
		t.Fatal("release must build modules independently of the caller's workspace")
	}
}
