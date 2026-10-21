package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"go.yaml.in/yaml/v3"
)

func TestLinuxContainerConfigurationUsesBuiltinPlugins(t *testing.T) {
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
			if c.Storage.Backend != "sqlite" || c.Storage.Executable != "" || c.Storage.SHA256 != "" || len(c.Storage.Args) != 0 {
				t.Fatal("container configuration did not select builtin SQLite")
			}
			if c.DataRoot != "/var/lib/musicfs/data" || !c.Plugins[0].InProcess() || c.Plugins[0].Executable != "" {
				t.Fatal("container storage rewrite changed unrelated deployment paths")
			}
		})
	}
}
