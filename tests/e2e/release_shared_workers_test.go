package e2e_test

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

func TestActualInstallationSharedStorageAndDSP(t *testing.T) {
	bundle := os.Getenv("MUSICFS_RELEASE_ROOT")
	if bundle == "" {
		t.Skip("set MUSICFS_RELEASE_ROOT to a completed installation bundle")
	}
	installed := copyInstallation(t, bundle)
	cfg, err := config.Load(filepath.Join(installed, "configs", "server.example.yaml"), map[string]string{
		"ADMIN_TOKEN": "release-worker-validation-credential-only",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertInstallationPersistence(t, installed, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Run("storage-sqlite", func(t *testing.T) {
		store, err := storage.Open(ctx, "plugin", storage.Options{
			DataRoot: t.TempDir(), Executable: cfg.Storage.Executable, Args: cfg.Storage.Args, SHA256: cfg.Storage.SHA256,
		})
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		session := auth.Session{ID: "installation-storage-roundtrip", User: auth.User{ID: "admin", Role: "admin"}, ExpiresAt: time.Now().Add(time.Hour)}
		if err := store.SaveAuth(ctx, session); err != nil {
			t.Fatal(err)
		}
		got, err := store.FindAuth(ctx, session.ID)
		if err != nil || got.ID != session.ID || got.User.ID != session.User.ID {
			t.Fatalf("packaged storage did not persist the session: %+v: %v", got, err)
		}
	})
	t.Run("dsp-gain", func(t *testing.T) {
		process, err := dspworker.New(ctx, dspworker.Config{
			Name: "musicfs.dsp.gain", Executable: cfg.Storage.Executable, SHA256: cfg.Storage.SHA256,
			Args: []string{"--plugin", "dsp-gain"},
		}, map[string]float64{"gainDB": -6})
		if err != nil {
			t.Fatal(err)
		}
		defer process.Close()
		block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1}, Frames: 1, Sequence: 7, PositionFrames: 42}
		if err := process.Process(&block); err != nil {
			t.Fatal(err)
		}
		gain := math.Pow(10, -6.0/20)
		if math.Abs(float64(block.Samples[0])-gain) > 0.000001 || math.Abs(float64(block.Samples[1])+gain) > 0.000001 || block.Sequence != 7 || block.PositionFrames != 42 {
			t.Fatalf("packaged DSP changed PCM timing or gain: %+v", block)
		}
	})
}
