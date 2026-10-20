package e2e_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/config"
)

func assertInstallationPersistence(t *testing.T, installed string, cfg config.Config) {
	t.Helper()
	program := "musicfs-plugin-storage-sqlite"
	if runtime.GOOS == "windows" {
		program += ".exe"
	}
	path := filepath.Join(installed, "bin", program)
	if cfg.Storage.Backend != "plugin" || filepath.Clean(cfg.Storage.Executable) != filepath.Clean(path) {
		t.Fatal("installation does not default to its independent SQLite worker")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if cfg.Storage.SHA256 != hex.EncodeToString(hash[:]) {
		t.Fatal("installation does not pin the supplied SQLite worker checksum")
	}
	if len(cfg.AudioDSP.Chain) != 0 || len(cfg.AudioDSP.Plugins) != 0 {
		t.Fatal("installation unexpectedly enables optional DSP effects")
	}
	raw, err = os.ReadFile(filepath.Join(installed, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Components []struct {
			Program, PluginID, Version string
			Required                   bool
		}
	}
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	storageRequired, compatibleAudio := false, false
	for _, item := range manifest.Components {
		if item.Program == "musicfs-plugin-storage-sqlite" {
			storageRequired = item.Required && item.PluginID == ""
		}
		if item.PluginID == "musicfs.plugin.decoder.ffmpeg" {
			compatibleAudio = item.Version == "0.2.0"
		}
	}
	if !storageRequired || !compatibleAudio {
		t.Fatal("installation manifest does not require SQLite or the DSP-aware audio worker")
	}
}
