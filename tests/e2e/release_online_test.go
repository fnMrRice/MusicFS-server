package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

// Exercise shipped executables without rebuilding them or contacting a platform.
func TestActualInstallationOnlineProgramsAndModules(t *testing.T) {
	bundle := os.Getenv("MUSICFS_RELEASE_ROOT")
	if bundle == "" {
		t.Skip("set MUSICFS_RELEASE_ROOT to a completed installation bundle")
	}
	installed := copyInstallation(t, bundle)
	cfg, err := config.Load(filepath.Join(installed, "configs", "server.example.yaml"), map[string]string{
		"ADMIN_TOKEN": "release-online-validation-credential-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertInstallationPersistence(t, installed, cfg)
	count := 0
	for _, plugin := range cfg.Plugins {
		platform := config.OnlinePlatform(plugin.PluginID)
		if platform == "" {
			continue
		}
		count++
		if len(plugin.Args) != 0 || len(plugin.Instances) != 1 || !plugin.CanStart(plugin.Instances[0]) {
			t.Fatalf("packaged online provider is not independently configured: %+v", plugin)
		}
		t.Run(platform+"/standalone", func(t *testing.T) {
			assertPackagedOnlineWorker(t, plugin, plugin.Executable, nil)
		})
		t.Run(platform+"/unified", func(t *testing.T) {
			path := filepath.Join(filepath.Dir(plugin.Executable), "musicfs-plugin-online"+filepath.Ext(plugin.Executable))
			assertPackagedOnlineWorker(t, plugin, path, []string{"--plugin", platform})
		})
	}
	if count != 3 {
		t.Fatalf("packaged online provider count = %d, want 3", count)
	}
	raw, err := os.ReadFile(filepath.Join(installed, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Modules []struct{ Path, Directory string }
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Modules) != 3 {
		t.Fatal("release manifest does not account for all three source modules")
	}
	for _, module := range manifest.Modules {
		if _, err := os.Stat(filepath.Join(installed, module.Directory, "go.mod")); err != nil {
			t.Fatalf("source module manifest is missing: %s: %v", module.Path, err)
		}
	}
}

func assertPackagedOnlineWorker(t *testing.T, plugin config.Plugin, path string, args []string) {
	t.Helper()
	platform := config.OnlinePlatform(plugin.PluginID)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	worker, err := pluginhost.StartOnline(context.Background(), pluginhost.OnlineConfig{
		Executable: path, Args: args, SHA256: hex.EncodeToString(digest[:]), StateDir: t.TempDir(),
		Platform: platform, UserID: "219460d0-7589-4c07-a50c-7481b83f0f79",
		InstanceID: plugin.Instances[0].InstanceID, Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("packaged %s worker did not handshake: %v", platform, err)
	}
	descriptor := worker.Descriptor()
	closeErr := worker.Close()
	if descriptor.Platform != platform || descriptor.Validate(platform) != nil || closeErr != nil {
		t.Fatalf("packaged %s worker descriptor or cleanup failed: %v", platform, closeErr)
	}
}
