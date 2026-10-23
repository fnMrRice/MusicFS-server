package backup_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	_ "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
	"github.com/fnMrRice/MusicFS-server/src/storage/backup"
)

func TestRestoreDropsDisposableProcessingIndexAndFiles(t *testing.T) {
	testPortableProcessingRestore(t, "", "mp3-128", ".mp3")
}

func TestSurroundBackupRestoreResetsOutputAndDropsProcessedCache(t *testing.T) {
	for _, layout := range []string{"5.1", "7.1"} {
		t.Run(layout, func(t *testing.T) {
			testPortableProcessingRestore(t, layout, "opus-"+layout, ".webm")
		})
	}
}

func testPortableProcessingRestore(t *testing.T, layout, profile, extension string) {
	t.Helper()
	doc, trackID, _ := installation(t)
	if layout != "" {
		tool, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		// 备份只校验配置，不执行音频工具；测试程序路径提供有效的现存文件。
		doc.Config.AudioChannels = config.AudioChannelsConfig{OutputLayout: layout}
		doc.Config.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.decoder.ffmpeg", Execution: "builtin", Enabled: true,
			Instances: []config.Instance{{InstanceID: "7edbaa88-7094-5657-ac43-37c118ef4f77", Enabled: true,
				Config: config.InstanceConfig{FFmpegPath: tool, FFprobePath: tool}}}}}
	}
	ctx := context.Background()
	store, err := sqlite.OpenMaintenance(ctx, filepath.Join(doc.Config.DataRoot, "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	payload := []byte("Disposable processing cache fixture")
	hash := sha256.Sum256(payload)
	digest := hex.EncodeToString(hash[:])
	i := processedcache.Identity{Owner: "admin", ProviderID: "local", SourceTrackID: trackID, PluginID: "musicfs.plugin.decoder.ffmpeg",
		AssetID: "stale-asset", Version: "v1", InputSHA256: digest, WorkerSHA256: digest,
		DecoderSHA256: digest, PluginVersion: "0.1.0", PipelineRevision: "gain-v1", Profile: profile}
	if layout != "" {
		i.PluginVersion = "0.3.0"
		i.PipelineRevision = "dsp-v1:" + digest
	}
	key, err := i.Key()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	r := processedcache.Record{ID: strings.Repeat("a", 48), Key: key, SHA256: digest,
		Identity: i, Size: int64(len(payload)), CreatedAt: now, LastUsedAt: now}
	if err = r.Validate(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	// Persist a complete but stale index, as can exist after its source changes.
	_, err = store.DB.ExecContext(ctx, "INSERT INTO processed_cache VALUES(?,?,?,?,?,?,?,?,?,?)",
		r.ID, r.Key, i.Owner, i.AssetID, i.Version, r.Size, r.SHA256,
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), raw)
	if err != nil {
		t.Fatal(err)
	}
	cacheFile := filepath.Join(doc.Config.CacheRoot, "processed", r.ID+extension)
	if err = os.MkdirAll(filepath.Dir(cacheFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cacheFile, payload, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	bundle, err := backup.Create(ctx, doc, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	restored, err := backup.Restore(ctx, bundle.Directory, filepath.Join(t.TempDir(), "restored"))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(restored.Configuration, map[string]string{"ADMIN_TOKEN": domain.NewID()})
	if err != nil || loaded.AudioChannels.OutputLayout != "stereo" || loaded.AudioChannels.RequiresProcessing() || len(loaded.AudioDSP.Chain) != 0 || len(loaded.AudioDSP.Plugins) != 0 {
		t.Fatalf("portable restore retained required audio processing: %+v %v", loaded.AudioChannels, err)
	}
	for _, plugin := range loaded.Plugins {
		if plugin.Enabled {
			t.Fatal("portable restore enabled an audio plugin")
		}
		for _, instance := range plugin.Instances {
			if instance.Enabled {
				t.Fatal("portable restore enabled an audio instance")
			}
		}
	}
	reopened, err := sqlite.OpenMaintenance(ctx, filepath.Join(restored.Directory, "data", "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var count int
	if err = reopened.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM processed_cache").Scan(&count); err != nil || count != 0 {
		t.Fatalf("disposable cache index restored: count=%d error=%v", count, err)
	}
	entries, err := os.ReadDir(filepath.Join(restored.Directory, "cache"))
	if err != nil || len(entries) != 0 {
		t.Fatal("disposable cache files restored", err)
	}
	if original, err := os.ReadFile(cacheFile); err != nil || string(original) != string(payload) {
		t.Fatal("source cache changed during restore", err)
	}
}
