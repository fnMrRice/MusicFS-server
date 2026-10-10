package bootstrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/api"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/identification/hashworker"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func checksum(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return ""
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}

func expectedChecksum(configured, path string) string {
	if configured != "" {
		return strings.ToLower(configured)
	}
	return checksum(path)
}

func compose(ctx context.Context, c config.Config, log *slog.Logger, instance string, ready func() bool) (*api.Server, func(), error) {
	store, err := sqlite.Open(ctx, filepath.Join(c.DataRoot, "musicfs.sqlite"))
	if err != nil {
		return nil, nil, err
	}
	mediaFiles, media, closeMedia, err := configureMedia(ctx, c, store)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	queue := jobs.New(store)
	runtimes, processor, decoder, ffprobe := configurePlugins(ctx, c, log)
	cleanup := func() {
		queue.Close()
		for _, r := range runtimes {
			if r.Worker != nil {
				r.Worker.Close()
			}
		}
		closeMedia()
		store.Close()
	}
	lib := library.New(store, queue, runtimes, metadataProbe(ffprobe, decoder))
	hashes, err := hashworker.New(store, lib, hashworker.Config{Auto: c.Hashing.Auto, MaxConcurrent: c.Hashing.MaxConcurrent, BytesPerSecond: c.Hashing.BytesPerSecond})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeHashes := cleanup
	cleanup = func() { hashes.Close(); closeBeforeHashes() }
	fingerprints, closeFingerprints := configureFingerprinting(ctx, c, log, store, lib, processor, media)
	closeBeforeFingerprints := cleanup
	cleanup = func() { closeFingerprints(); closeBeforeFingerprints() }
	content := mediastore.NewContent(store, store, mediaFiles)
	metadata, err := configureEnrichment(ctx, c, log, store, lib, content)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeMetadata := cleanup
	cleanup = func() { metadata.Close(); closeBeforeMetadata() }
	sessions := auth.New(store, c.AdminToken, c.AllowedOrigins, time.Duration(c.SessionTTLSeconds)*time.Second)
	handler := api.New(api.Options{Auth: sessions, InstanceID: instance, Ready: ready, SecureCookie: c.TLSCert != "", Logger: log})
	handler.RegisterLibrary(lib)
	handler.RegisterHashing(hashes)
	handler.RegisterFingerprinting(fingerprints)
	handler.RegisterMedia(content)
	handler.RegisterEnrichment(metadata)
	var fingerprintDetails matching.FingerprintDetails
	if fingerprints != nil {
		fingerprintDetails = fingerprints
	}
	handler.RegisterMatching(matching.New(store, fingerprintDetails))
	player, err := playback.New(ctx, store, lib, processor, playback.Config{CacheRoot: c.CacheRoot, MaxSessions: c.MaxSessions, MaxCacheBytes: c.MaxCacheBytes, NCMDecoder: decoder})
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	sessions.OnRevoke = player.Revoke
	handler.RegisterPlayback(player)
	previousCleanup := cleanup
	cleanup = func() { player.Close(); previousCleanup() }
	if err = queue.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = hashes.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	return handler, cleanup, nil
}
