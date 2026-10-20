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

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/api"
	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/catalog"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/identification/hashworker"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/lifecycle"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/personal"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/sourcedisplay"
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

func compose(ctx context.Context, c config.Config, log *slog.Logger, instance string, ready func() bool, lifecycleManagers ...*lifecycle.Manager) (*api.Server, func(), error) {
	c.NormalizeIdentities()
	if err := checkLegacyWritebackJournals(ctx, c.DataRoot); err != nil {
		return nil, nil, err
	}
	store, err := sqlite.Open(ctx, filepath.Join(c.DataRoot, "musicfs.sqlite"))
	if err != nil {
		return nil, nil, err
	}
	if err := store.MigratePluginInstanceIDs(ctx); err != nil {
		store.Close()
		return nil, nil, err
	}
	c, err = config.InitializeLocalDirectories(ctx, c)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	var configuration *config.Store
	if c.SourcePath != "" {
		configuration, err = config.OpenStore(ctx, c.SourcePath, c.SourceOverrides, store)
		if err != nil {
			store.Close()
			return nil, nil, err
		}
	}
	if configuration != nil {
		c = configuration.Current()
	}
	pluginManager, err := plugincontrol.New(ctx, c, configuration, store)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	mediaFiles, media, closeMedia, err := configureMedia(ctx, c, store)
	if err != nil {
		pluginManager.Close()
		store.Close()
		return nil, nil, err
	}
	queue := jobs.New(store)
	runtimes, processor, decoder, ffprobe := configurePlugins(ctx, c, log)
	closeManagedLibrary := func() {}
	closeManagedOnline := func() {}
	cleanup := func() {
		pluginManager.Close()
		queue.Close()
		closeManagedLibrary()
		closeManagedOnline()
		for _, r := range runtimes {
			if r.Worker != nil {
				r.Worker.Close()
			}
		}
		closeMedia()
		store.Close()
	}
	policy := writeBackPolicy(c, runtimes, ffprobe != "")
	probeState := &managedProbe{}
	probeState.update(ffprobe, decoder, audioGate(pluginManager, "musicfs.plugin.decoder.ffmpeg"), audioGate(pluginManager, "musicfs.plugin.decoder.ncm"))
	probe := library.Probe(probeState.probe)
	lib := library.New(store, queue, runtimes, probe)
	lib.SetProbeSession(probeState.session)
	lib.SetWriteBackProbeReady(probeState.ready)
	content := mediastore.NewContent(store, store, mediaFiles)
	writeProbe := library.Probe(probeState.writeProbe)
	writebacks, closeWritebacks, err := configureWriteBack(ctx, c, store, lib, content, runtimes, policy, writeProbe, probeState.writeSession)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeWritebacks := cleanup
	cleanup = func() { closeWritebacks(); closeBeforeWritebacks() }
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
	metadata, err := configureEnrichment(ctx, c, log, store, lib, content)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeMetadata := cleanup
	cleanup = func() { metadata.Close(); closeBeforeMetadata() }
	online, onlineRuntimes, closeOnline, err := configureOnline(ctx, c, log)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeOnline := cleanup
	cleanup = func() { closeOnline(); closeBeforeOnline() }
	recommendations, err := configureRecommendations(onlineRuntimes)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeRecommendations := cleanup
	cleanup = func() { _ = recommendations.Close(); closeBeforeRecommendations() }
	remotePlaylists, err := configureRemotePlaylists(ctx, store, online)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeRemotePlaylists := cleanup
	cleanup = func() { remotePlaylists.Close(); closeBeforeRemotePlaylists() }
	sessions := auth.New(store, c.AdminToken, c.AllowedOrigins, time.Duration(c.SessionTTLSeconds)*time.Second)
	handler := api.New(api.Options{Auth: sessions, InstanceID: instance, Ready: ready, SecureCookie: c.TLSCert != "", Logger: log})
	handler.RegisterLibrary(lib)
	handler.RegisterCatalog(catalog.New(store))
	handler.RegisterPersonal(personal.New(store))
	handler.RegisterSourceDisplay(sourcedisplay.New(store, online))
	handler.RegisterPluginManagement(pluginManager)
	handler.RegisterOnline(online)
	handler.RegisterRecommendations(recommendations)
	handler.RegisterRemotePlaylists(remotePlaylists)
	handler.RegisterPlaylists(playlists.NewAggregate(store))
	handler.RegisterHashing(hashes)
	handler.RegisterFingerprinting(fingerprints)
	handler.RegisterMedia(content)
	handler.RegisterAlbums(albums.New(store, content, lib))
	handler.RegisterEnrichment(metadata)
	handler.RegisterMetadataAutomation(metadata)
	handler.RegisterWriteBack(writebacks)
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
	if len(lifecycleManagers) > 0 && lifecycleManagers[0] != nil {
		manager := lifecycleManagers[0]
		handler.RegisterLifecycle(manager)
		sessions.OnRevoke = func(id string) { manager.RevokeSession(id); player.Revoke(id) }
	}
	handler.RegisterPlayback(player)
	previousCleanup := cleanup
	cleanup = func() { player.Close(); previousCleanup() }
	closeManagedOnline, err = registerOnlineManagement(ctx, c, pluginManager, online, onlineRuntimes, recommendations)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = registerScraperManagement(pluginManager, c, metadata); err != nil {
		cleanup()
		return nil, nil, err
	}
	updateBackground := backgroundAudioUpdate(pluginManager, probeState, metadata, fingerprints, log)
	if err = configureAudioManagement(c, pluginManager, player, processor, decoder, updateBackground); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = updateBackground(ctx, processor, decoder, ffprobe); err != nil {
		cleanup()
		return nil, nil, err
	}
	closeManagedLibrary, err = configureLibraryManagement(ctx, c, lib, pluginManager, log)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	pluginManager.Start()
	beforeManager := cleanup
	cleanup = func() { pluginManager.Close(); beforeManager() }
	if err = writebacks.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = queue.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = hashes.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	// Recovery workers may claim persisted tasks immediately. Attach every
	// runtime gate and dependency before allowing that first claim.
	if fingerprints != nil {
		if err = fingerprints.Start(ctx); err != nil {
			cleanup()
			return nil, nil, err
		}
	}
	if err = metadata.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	if err = remotePlaylists.Start(ctx); err != nil {
		cleanup()
		return nil, nil, err
	}
	return handler, cleanup, nil
}
