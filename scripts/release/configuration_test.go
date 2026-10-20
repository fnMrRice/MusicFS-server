package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"go.yaml.in/yaml/v3"
)

func TestPackagedConfigurationStartsWithoutImportDirectories(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"windows", "linux"} {
		t.Run(target, func(t *testing.T) {
			output := t.TempDir()
			if err := os.Mkdir(filepath.Join(output, "configs"), 0700); err != nil {
				t.Fatal(err)
			}
			program := "musicfs-plugin-storage-sqlite"
			if target == "windows" {
				program += ".exe"
			}
			if err := os.Mkdir(filepath.Join(output, "bin"), 0700); err != nil {
				t.Fatal(err)
			}
			worker := []byte("required SQLite worker fixture")
			if err := os.WriteFile(filepath.Join(output, "bin", program), worker, 0700); err != nil {
				t.Fatal(err)
			}
			b := builder{root: repository, out: output, target: target, web: "fixture-web"}
			if err := b.configuration(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(output, "configs", "server.example.yaml")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var supplied config.Config
			if err := yaml.Unmarshal(raw, &supplied); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(worker)
			if supplied.Storage.Backend != "plugin" || supplied.Storage.Executable != "../bin/"+program || supplied.Storage.SHA256 != hex.EncodeToString(hash[:]) {
				t.Fatal("packaged defaults do not require the exact bundled SQLite worker")
			}
			if len(supplied.AudioDSP.Chain) != 0 || len(supplied.AudioDSP.Plugins) != 0 {
				t.Fatal("packaged defaults unexpectedly enable a DSP plugin")
			}
			found := false
			for _, plugin := range supplied.Plugins {
				if plugin.PluginID != "musicfs.plugin.source.localfiles" {
					continue
				}
				found = true
				for _, instance := range plugin.Instances {
					if instance.Permissions != nil || len(instance.Config.MusicRoots) != 0 {
						t.Fatal("packaged defaults must not override directory access or preselect imports")
					}
				}
			}
			if !found || strings.Contains(string(raw), "../tests/testdata/audio") || strings.Contains(string(raw), "../music") {
				t.Fatal("packaged local configuration is missing or contains a sample music directory")
			}
			doc, err := config.ParseDocument(raw, path, map[string]string{"ADMIN_TOKEN": "packaged-config-test-credential-only"})
			if err != nil {
				t.Fatal(err)
			}
			for _, plugin := range doc.Config.Plugins {
				if plugin.PluginID != "musicfs.plugin.source.localfiles" {
					continue
				}
				for _, instance := range plugin.Instances {
					if len(instance.Config.MusicRoots) != 0 || !instance.PendingConfiguration {
						t.Fatal("fresh installation unexpectedly selected a music directory")
					}
					if got := config.AllowedDirectories(instance); !reflect.DeepEqual(got, []string{filepath.Join(home, "Music")}) {
						t.Fatalf("packaged configuration did not use default Music access: %v", got)
					}
				}
			}
			if _, err := os.Stat(filepath.Join(output, "music")); !os.IsNotExist(err) {
				t.Fatalf("configuration created an unsolicited music directory: %v", err)
			}
		})
	}
}

func TestPackagedConfigurationRejectsMissingRequiredStorage(t *testing.T) {
	repository, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	if err := os.Mkdir(filepath.Join(output, "configs"), 0700); err != nil {
		t.Fatal(err)
	}
	b := builder{root: repository, out: output, target: "windows"}
	if err := b.configuration(); err == nil {
		t.Fatal("release configuration accepted a missing mandatory SQLite worker")
	}
}
