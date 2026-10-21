package e2e_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
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
		if !slices.Equal(plugin.Args, []string{"--plugin", platform}) || len(plugin.Instances) != 1 || !plugin.CanStart(plugin.Instances[0]) {
			t.Fatalf("packaged online provider is not configured for the shared executable: %+v", plugin)
		}
		t.Run(platform+"/bundle", func(t *testing.T) {
			assertPackagedOnlineWorker(t, plugin, plugin.Executable, plugin.Args)
		})
		for _, alternative := range []struct {
			name string
			args []string
		}{{"musicfs-plugin-" + platform, nil}, {"musicfs-plugin-online", []string{"--plugin", platform}}} {
			path := filepath.Join(filepath.Dir(plugin.Executable), alternative.name+filepath.Ext(plugin.Executable))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				continue
			} else if err != nil {
				t.Fatal(err)
			}
			t.Run(platform+"/"+alternative.name, func(t *testing.T) {
				assertPackagedOnlineWorker(t, plugin, path, alternative.args)
			})
		}
	}
	if count != 3 {
		t.Fatalf("packaged online provider count = %d, want 3", count)
	}
	raw, err := os.ReadFile(filepath.Join(installed, "release.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Programs   []string
		Modules    []struct{ Path, Directory string }
		Components []struct {
			Program string
			Args    []string
		}
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Modules) != 3 {
		t.Fatal("release manifest does not account for all three source modules")
	}
	if len(manifest.Programs) != 2 && len(manifest.Programs) != 8 {
		t.Fatalf("installation has neither default nor split program catalog: %v", manifest.Programs)
	}
	binaries, err := os.ReadDir(filepath.Join(installed, "bin"))
	if err != nil || len(binaries) != len(manifest.Programs) {
		t.Fatalf("binary directory does not match the release program catalog: %v", err)
	}
	for _, program := range manifest.Programs {
		name := program
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		info, err := os.Stat(filepath.Join(installed, "bin", name))
		if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
			t.Fatalf("packaged program is not a nonempty regular file: %s: %v", program, err)
		}
	}
	roles := map[string]bool{}
	for _, component := range manifest.Components {
		if component.Program != "musicfs-plugins" {
			continue
		}
		if len(component.Args) != 2 || component.Args[0] != "--plugin" || roles[component.Args[1]] {
			t.Fatalf("invalid or duplicated shared role: %+v", component)
		}
		roles[component.Args[1]] = true
	}
	if len(roles) != 9 {
		t.Fatalf("shared role count = %d, want 9", len(roles))
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
