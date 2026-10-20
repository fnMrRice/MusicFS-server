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
	for _, program := range releasePrograms {
		if !registered[program.name] {
			t.Fatalf("packaged executable lacks release metadata: %s", program.name)
		}
		if _, err := os.Stat(filepath.Join(repository, program.moduleDir, program.packagePath, "main.go")); err != nil {
			t.Fatalf("packaged executable has no entry point: %s: %v", program.name, err)
		}
	}
	for _, program := range []string{"musicfs-plugin-storage-sqlite", "musicfs-plugin-dsp-gain", "musicfs-plugin-netease", "musicfs-plugin-qqmusic", "musicfs-plugin-applemusic"} {
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

func TestBundledRolesExcludeOnlineImplementations(t *testing.T) {
	count, online := 0, 0
	for _, item := range components() {
		if item.Program == "musicfs-plugins" {
			count++
			if len(item.Args) != 2 || item.Args[0] != "--plugin" ||
				!slices.Contains([]string{"local", "audio-ffmpeg", "ncm", "scraper-local"}, item.Args[1]) {
				t.Fatalf("unexpected bundled role: %+v", item)
			}
		}
		if item.Program == "musicfs-plugin-online" {
			online++
			if len(item.Args) != 2 || item.Args[0] != "--plugin" ||
				!slices.Contains([]string{"netease", "qqmusic", "applemusic"}, item.Args[1]) ||
				item.PluginID != "musicfs.plugin.source."+item.Args[1] {
				t.Fatalf("unexpected unified online role: %+v", item)
			}
		}
	}
	if count != 4 {
		t.Fatalf("bundled role count = %d, want 4", count)
	}
	if online != 3 {
		t.Fatalf("unified online role count = %d, want 3", online)
	}
	t.Setenv("GOWORK", "/untrusted/go.work")
	got := targetEnvironment("windows", "amd64")
	if !slices.Contains(got, "GOWORK=off") || slices.Contains(got, "GOWORK=/untrusted/go.work") {
		t.Fatal("release must build modules independently of the caller's workspace")
	}
}
