package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"go.yaml.in/yaml/v3"
)

func TestLinuxContainerConfigurationPinsPackagedSQLite(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "deploy", "server.container.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"linux", "windows"} {
		t.Run(target, func(t *testing.T) {
			output := t.TempDir()
			path := filepath.Join(output, "deploy", "server.container.example.yaml")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, source, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(output, "bin"), 0700); err != nil {
				t.Fatal(err)
			}
			worker := []byte("Linux SQLite worker binary fixture")
			if err := os.WriteFile(filepath.Join(output, "bin", "musicfs-plugins"), worker, 0700); err != nil {
				t.Fatal(err)
			}
			b := builder{out: output, target: target}
			if err := b.containerConfiguration(); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if target == "windows" {
				if !bytes.Equal(raw, source) {
					t.Fatal("Windows package rewrote the Linux source compatibility example")
				}
				return
			}
			var c config.Config
			if err := yaml.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			hash := sha256.Sum256(worker)
			if c.Storage.Backend != "plugin" || c.Storage.Executable != "/opt/musicfs/bin/musicfs-plugins" || c.Storage.SHA256 != hex.EncodeToString(hash[:]) || !slices.Equal(c.Storage.Args, []string{"--plugin", "storage-sqlite"}) {
				t.Fatal("container configuration did not pin its Linux SQLite worker")
			}
			if c.DataRoot != "/var/lib/musicfs/data" || c.Plugins[0].Executable != "/opt/musicfs/bin/musicfs-plugins" {
				t.Fatal("container storage rewrite changed unrelated deployment paths")
			}
		})
	}
}
