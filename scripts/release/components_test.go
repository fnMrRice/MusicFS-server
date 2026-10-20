package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestComponentsAndBuildScriptsIncludeStandaloneExtensions(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool)
	for _, item := range components() {
		if !slices.Contains(programs, item.Program) {
			t.Fatalf("component has no packaged executable: %s", item.Program)
		}
		registered[item.Program] = true
	}
	for _, program := range programs {
		if !registered[program] {
			t.Fatalf("packaged executable lacks release metadata: %s", program)
		}
		if _, err := os.Stat(filepath.Join(repository, "src", "app", program, "main.go")); err != nil {
			t.Fatalf("packaged executable has no entry point: %s: %v", program, err)
		}
	}
	for _, program := range []string{"musicfs-plugin-storage-sqlite", "musicfs-plugin-dsp-gain"} {
		if !slices.Contains(programs, program) {
			t.Fatalf("standalone extension omitted from package: %s", program)
		}
		for _, item := range components() {
			if item.Program != program {
				continue
			}
			if len(item.Args) != 0 {
				t.Fatalf("standalone extension must not use bundled role dispatch: %+v", item)
			}
			if program == "musicfs-plugin-storage-sqlite" && (!item.Required || item.PluginID != "") {
				t.Fatalf("SQLite worker must be mandatory and outside the toggleable plugin registry: %+v", item)
			}
			if program == "musicfs-plugin-dsp-gain" && (item.Required || !slices.Contains(item.Requirements, "disabled by default")) {
				t.Fatalf("DSP example must remain optional: %+v", item)
			}
		}
	}
	for _, path := range []string{"scripts/build.ps1", "scripts/build.sh"} {
		raw, err := os.ReadFile(filepath.Join(repository, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
		for _, program := range programs {
			if !strings.Contains(string(raw), program) {
				t.Fatalf("%s omits packaged executable %s", path, program)
			}
		}
	}
}

func TestBundledRolesRemainAvailable(t *testing.T) {
	count := 0
	for _, item := range components() {
		if item.Program == "musicfs-plugins" {
			count++
			if len(item.Args) != 2 || item.Args[0] != "--plugin" ||
				!slices.Contains([]string{"local", "audio-ffmpeg", "ncm", "scraper-local", "netease", "qqmusic", "applemusic"}, item.Args[1]) {
				t.Fatalf("unexpected bundled role: %+v", item)
			}
		}
	}
	if count != 7 {
		t.Fatalf("bundled role count = %d, want 7", count)
	}
}
