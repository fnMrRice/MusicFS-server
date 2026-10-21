package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestBackupCLIStopsLiveAccessAndRestoresActualServer(t *testing.T) {
	server, provider := build(t, "musicfs"), build(t, "musicfs-plugin-local")
	cfg, base := yamlServiceConfig(t)
	music := t.TempDir()
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(music, "fixture.wav")
	if err = os.WriteFile(source, payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Synthetic music", Path: music}}}}}}}
	configPath := writeYAMLConfiguration(t, cfg)
	parent := t.TempDir()
	cli := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, server, args...)
		for _, env := range os.Environ() {
			if !strings.HasPrefix(strings.ToUpper(env), "MUSICFS_") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		return cmd.CombinedOutput()
	}
	var objectID string
	t.Run("live-server-lock", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		track := scanNCM(t, c)["fixture.wav"]
		object := uploadActualMedia(t, c, mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}, "lyrics", "en", "text/plain", []byte("Preserved manual lyrics\n"), "backup-cli-manual-lyrics")
		objectID = object.ID
		if raw, err := cli("backup", "--config", configPath, "--output", parent); err == nil || !bytes.Contains(raw, []byte("Stop the server")) {
			t.Fatalf("live backup accepted: %v %s", err, raw)
		}
	})
	raw, err := cli("backup", "--config", configPath, "--output", parent)
	var bundle backup.Result
	if err != nil || json.Unmarshal(raw, &bundle) != nil || bundle.Directory == "" {
		t.Fatalf("backup CLI: %v %s", err, raw)
	}
	if raw, err = cli("verify-backup", "--input", bundle.Directory); err != nil {
		t.Fatalf("verify CLI: %v %s", err, raw)
	}
	destination := filepath.Join(t.TempDir(), "new-installation")
	raw, err = cli("restore", "--input", bundle.Directory, "--destination", destination)
	var restored backup.RestoreResult
	if err != nil || json.Unmarshal(raw, &restored) != nil {
		t.Fatalf("restore CLI: %v %s", err, raw)
	}
	loaded, err := config.Load(restored.Configuration, map[string]string{"ADMIN_TOKEN": domain.NewID()})
	if err != nil {
		t.Fatal(err)
	}
	fresh, newBase := yamlServiceConfig(t)
	loaded.Listen = fresh.Listen
	loaded.AllowedOrigins = fresh.AllowedOrigins
	t.Run("restored-server", func(t *testing.T) {
		startNCMServer(t, loaded, newBase)
		c := login(t, newBase, loaded.AdminToken)
		code, raw, _ := c.request(t, "GET", "/api/v1/media/"+objectID, nil, "")
		if code != 200 || string(raw) != "Preserved manual lyrics\n" {
			t.Fatalf("restored content: %d %s", code, raw)
		}
		code, raw, _ = c.request(t, "GET", "/api/v1/provider-instances", nil, "")
		if code != 200 || !bytes.Contains(raw, []byte(`"availability":"offline"`)) {
			t.Fatalf("restored provider enabled: %d %s", code, raw)
		}
	})
	after, err := os.ReadFile(source)
	if err != nil || !bytes.Equal(after, payload) {
		t.Fatal("backup or restore changed music")
	}
	if err = os.WriteFile(filepath.Join(loaded.DataRoot, ".restore-incomplete"), []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = bootstrap.Serve(context.Background(), loaded, slog.New(slog.NewTextHandler(io.Discard, nil))); err == nil || !strings.Contains(err.Error(), "restore is incomplete") {
		t.Fatal("partial restore started", err)
	}
}
