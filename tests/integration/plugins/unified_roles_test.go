package plugins_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

func TestUnifiedDatabaseDSPAndOnlineProcessesRemainIndependent(t *testing.T) {
	path := onlineBinary(t, "./src/app/musicfs-plugins", "unified plugins")
	cfg := onlineConfig(t, path, online.Netease)
	ctx := context.Background()
	store, err := storage.Open(ctx, "plugin", storage.Options{
		DataRoot: t.TempDir(), Executable: path, SHA256: cfg.SHA256,
		Args: []string{"--plugin", "storage-sqlite"},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	node, err := dspworker.New(ctx, dspworker.Config{
		Name: "musicfs.dsp.gain", Executable: path, SHA256: cfg.SHA256,
		Args: []string{"--plugin", "dsp-gain"}, TimeoutMs: 5000,
	}, map[string]float64{"gainDB": -6})
	if err != nil {
		t.Fatal(err)
	}
	defer node.Close()
	cfg.Args = []string{"--plugin", online.Netease}
	source := onlineStart(t, cfg)
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}

	session := auth.Session{ID: "unified-fixture", User: auth.User{ID: "admin", Role: "admin"}, ExpiresAt: time.Now().Add(time.Hour)}
	if err = store.SaveAuth(ctx, session); err != nil {
		t.Fatal("online shutdown affected the database", err)
	}
	if saved, err := store.FindAuth(ctx, session.ID); err != nil || saved.User.ID != "admin" {
		t.Fatal("unified database did not persist the session", saved, err)
	}
	block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1}, Frames: 1, Sequence: 7, PositionFrames: 4800, End: true}
	if err = node.Process(&block); err != nil {
		t.Fatal("online shutdown affected DSP", err)
	}
	if math.Abs(float64(block.Samples[0])-math.Pow(10, -6.0/20)) > 0.000001 ||
		block.Sequence != 7 || block.PositionFrames != 4800 || !block.End {
		t.Fatal("unified DSP changed the signal or timing unexpectedly", block)
	}
	if err = node.Drain(); err != nil {
		t.Fatal(err)
	}
}
