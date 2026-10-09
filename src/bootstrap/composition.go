package bootstrap

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/fnMrRice/MusicFS-server/src/api"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
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
	queue := jobs.New(store)
	var runtimes []library.Runtime
	cleanup := func() {
		queue.Close()
		for _, r := range runtimes {
			if r.Worker != nil {
				r.Worker.Close()
			}
		}
		store.Close()
	}
	for _, root := range c.MusicRoots {
		r := library.Runtime{ID: root.ID, Name: root.Name, Owner: "admin"}
		r.Worker, err = pluginhost.Start(ctx, pluginhost.Config{Executable: c.PluginPath, SHA256: expectedChecksum(c.PluginSHA256, c.PluginPath), InstanceID: root.ID, UserID: "admin", Roots: []sourceaccess.RootConfig{{ID: root.ID, Path: root.Path}}, StateDir: filepath.Join(c.DataRoot, "providers", root.ID)})
		if err != nil {
			log.Warn("provider unavailable", "providerId", root.ID)
		}
		runtimes = append(runtimes, r)
	}
	var decoder *ncmdecoder.Processor
	if c.NCMWorkerPath != "" {
		decoder, err = ncmdecoder.NewProcessor(ncmdecoder.Config{WorkerPath: c.NCMWorkerPath, WorkerSHA256: expectedChecksum(c.NCMWorkerSHA256, c.NCMWorkerPath), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
		if err != nil {
			log.Warn("NCM decryption unavailable")
		}
	}
	lib := library.New(store, queue, runtimes, metadataProbe(c.FFprobePath, decoder))
	sessions := auth.New(store, c.AdminToken, c.AllowedOrigins, time.Duration(c.SessionTTLSeconds)*time.Second)
	handler := api.New(api.Options{Auth: sessions, InstanceID: instance, Ready: ready, SecureCookie: c.TLSCert != "", Logger: log})
	handler.RegisterLibrary(lib)
	var processor *audioffmpeg.Processor
	if c.AudioWorkerPath != "" && c.FFmpegPath != "" && c.FFprobePath != "" {
		processor, err = audioffmpeg.NewProcessor(ctx, audioffmpeg.Config{FFmpegPath: c.FFmpegPath, FFprobePath: c.FFprobePath, WorkerPath: c.AudioWorkerPath, WorkerSHA256: expectedChecksum(c.AudioWorkerSHA256, c.AudioWorkerPath), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
		if err != nil {
			log.Warn("audio processing unavailable")
		}
	}
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
	return handler, cleanup, nil
}
