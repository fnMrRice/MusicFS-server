package playback_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/processedcache"
)

// These are repository authorization tests over real scanned assets. The
// synthetic cache record is not evidence that an audio processor was executed.
func cacheRecord(f recoveryFixture, d playback.Description) (processedcache.Record, processedcache.Authorization) {
	t := f.tracks[1]
	i := processedcache.Identity{Owner: f.who.User.ID, ProviderID: t.ProviderID, SourceTrackID: t.ID, AssetID: t.AssetID, Version: t.Version, InputSHA256: strings.Repeat("1", 64), WorkerSHA256: strings.Repeat("2", 64), FFmpegSHA256: strings.Repeat("3", 64), PluginVersion: "0.1.0", PipelineRevision: "gain-v1", Profile: "mp3-128"}
	key, _ := i.Key()
	now := time.Now().UTC()
	r := processedcache.Record{ID: domain.NewID(), Key: key, Identity: i, Size: 8, SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("testonly"))), CreatedAt: now, LastUsedAt: now}
	a := processedcache.Authorization{Owner: f.who.User.ID, AuthID: f.who.ID, SessionID: d.SessionID, AttemptID: d.AttemptID}
	return r, a
}

func TestProcessedCacheRepositoryRechecksCurrentAuthorization(t *testing.T) {
	for _, change := range []string{"owner", "auth", "attempt", "stop", "asset"} {
		t.Run(change, func(t *testing.T) {
			f := recoverySetup(t, nil, 0)
			f.request.SourceTrackID = f.tracks[1].ID
			d := f.create(t, "cache-repository-create")
			r, a := cacheRecord(f, d)
			ctx := context.Background()
			if err := f.repo.PublishProcessedCache(ctx, r, a); err != nil {
				t.Fatal(err)
			}
			if err := f.repo.CheckProcessedCache(ctx, r, a); err != nil {
				t.Fatal(err)
			}
			want := "CONFLICT"
			switch change {
			case "owner":
				a.Owner = "another-owner"
				want = "ACCESS_DENIED"
			case "auth":
				if err := f.repo.DeleteAuth(ctx, f.who.ID); err != nil {
					t.Fatal(err)
				}
				want = "AUTH_REQUIRED"
			case "attempt":
				if _, err := f.player.Control(ctx, f.who, d.SessionID, "seek", playback.ControlRequest{AttemptID: d.AttemptID, PositionMs: 1000}, "cache-repository-seek"); err != nil {
					t.Fatal(err)
				}
			case "stop":
				if err := f.player.Stop(ctx, f.who, d.SessionID); err != nil {
					t.Fatal(err)
				}
				want = "SESSION_EXPIRED"
			case "asset":
				rescanRecoveryTarget(t, f)
				want = "RESOURCE_CHANGED"
			}
			recoveryError(t, f.repo.CheckProcessedCache(ctx, r, a), want)
			late := r
			late.ID = domain.NewID()
			recoveryError(t, f.repo.PublishProcessedCache(ctx, late, a), want)
			items, err := f.repo.ListProcessedCache(ctx)
			if err != nil || len(items) != 1 {
				t.Fatal("failed publication changed completed cache index", err)
			}
		})
	}
}

func TestProcessedCacheRetainsUnlinkedInputBudgetUntilReaderCloses(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	f.player.Close()
	info, err := os.Stat(f.path)
	if err != nil {
		t.Fatal(err)
	}
	f.player, err = playback.New(context.Background(), f.repo.Store, f.repo.library, &audioffmpeg.Processor{}, playback.Config{CacheRoot: filepath.Join(filepath.Dir(f.path), "cache-retired"), MaxSessions: 8, MaxCacheBytes: 2 * info.Size()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.player.Close)
	d := f.create(t, "cache-reader-original")
	s, err := f.player.OpenStream(context.Background(), f.who, d.SessionID, d.AttemptID, false)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = f.player.Stop(context.Background(), f.who, d.SessionID); err != nil {
		t.Fatal(err)
	}
	// Two new input snapshots cannot both fit while a retired reader is held.
	f.create(t, "cache-reader-second")
	_, err = f.player.Create(context.Background(), f.who, f.request, "cache-reader-third")
	recoveryError(t, err, "RATE_LIMITED")
	s.Close()
	f.create(t, "cache-reader-after-close")
}
