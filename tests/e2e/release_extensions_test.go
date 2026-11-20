package e2e_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

func assertInstallationPersistence(t *testing.T, installed string, cfg config.Config) {
	t.Helper()
	if cfg.Storage.Backend != "sqlite" || cfg.Storage.Execution == "process" || cfg.Storage.Executable != "" || cfg.Storage.SHA256 != "" || len(cfg.Storage.Args) != 0 {
		t.Fatal("installation does not default to builtin SQLite without an executable")
	}
	if len(cfg.AudioDSP.Chain) != 0 || len(cfg.AudioDSP.Plugins) != 0 {
		t.Fatal("installation unexpectedly enables optional DSP effects")
	}
	raw, err := os.ReadFile(filepath.Join(installed, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Components []struct {
			Program, PluginID, Version, Execution string
			Required                              bool
			Args                                  []string
		}
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	storageRequired, compatibleAudio := false, false
	algorithms := map[string]bool{
		"musicfs.plugin.analysis.chromaprint": false,
		"musicfs.plugin.tags.flac":            false,
		"musicfs.plugin.matching.standard":    false,
		"musicfs.plugin.transform.thumbnail":  false,
	}
	for _, item := range manifest.Components {
		if item.Execution == "process" && item.Required {
			t.Fatal("installation makes an optional external plugin process mandatory")
		}
		if item.Program != "musicfs" || item.Execution != "builtin" {
			continue
		}
		if item.PluginID == "musicfs.plugin.storage.sqlite" {
			storageRequired = item.Required && len(item.Args) == 0
		}
		if item.PluginID == "musicfs.plugin.decoder.ffmpeg" {
			compatibleAudio = pluginproto.ValidPluginVersion(item.Version)
		}
		if _, ok := algorithms[item.PluginID]; ok {
			if algorithms[item.PluginID] || !pluginproto.ValidPluginVersion(item.Version) || len(item.Args) != 0 {
				t.Fatalf("invalid builtin algorithm component: %+v", item)
			}
			algorithms[item.PluginID] = true
		}
	}
	if !storageRequired || !compatibleAudio {
		t.Fatal("installation manifest does not require builtin SQLite or declare builtin audio")
	}
	for id, found := range algorithms {
		if !found {
			t.Fatal("installation manifest omits a builtin algorithm", id)
		}
	}
}
