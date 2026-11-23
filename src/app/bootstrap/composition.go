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

	_ "github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/lifecycle"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library/musicdirs"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/library/sourcedisplay"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/hashworker"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/downloadjobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/codecsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	_ "github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
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

// pluginChecksum 将内置实现版本纳入实例修订，避免依赖已经不执行的旧程序文件。
func pluginChecksum(p config.Plugin) string {
	if p.InProcess() {
		declaration, _ := pluginconfig.Lookup(p.PluginID)
		return fmt.Sprintf("%x", sha256.Sum256([]byte(p.PluginID+"@"+declaration.Version+"/builtin")))
	}
	return expectedChecksum(p.SHA256, p.Executable)
}

func compose(ctx context.Context, c config.Config, log *slog.Logger, instance string, ready func() bool, lifecycleManagers ...*lifecycle.Manager) (*api.Server, func(), error) {
	c.NormalizeIdentities()
	c.NormalizeRuntime()
	if err := checkLegacyWritebackJournals(ctx, c.DataRoot); err != nil {
		return nil, nil, err
	}
	store, err := storage.Open(ctx, c.Storage.Backend, storage.Options{
		DataRoot: c.DataRoot, Values: c.Storage.Options,
		Executable: c.Storage.Executable, Args: c.Storage.Args, SHA256: c.Storage.SHA256,
	})
	if err != nil {
		return nil, nil, err
	}
	c, err = config.InitializeLocalDirectories(ctx, c, musicdirs.Discover)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	var configuration *config.Store
	if c.SourcePath != "" {
		configuration, err = config.OpenStoreForStartup(ctx, c.SourcePath, c.SourceOverrides, store, c)
		if err != nil {
			store.Close()
			return nil, nil, err
		}
	}
	if configuration != nil {
		current := configuration.Current()
		if !config.SameStartupSettings(current, c) {
			store.Close()
			return nil, nil, fmt.Errorf("Storage configuration changed during startup; restart with the current configuration")
		}
		c = current
	}
	if err = store.BindMusicSourcePlugins(ctx, musicSourcePlugins(c)); err != nil {
		store.Close()
		return nil, nil, err
	}
	storageDriver, err := storage.Lookup(c.Storage.Backend)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	pluginManager, err := plugincontrol.New(ctx, c, configuration, store, storageDriver.PluginVersion)
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
	if (c.AudioDSP.Enabled && len(c.AudioDSP.Chain) > 0 || c.AudioChannels.RequiresProcessing()) && processor == nil {
		cleanup()
		return nil, nil, fmt.Errorf("Configured DSP pipeline is unavailable")
	}
	policy := writeBackPolicy(c, runtimes, ffprobe != nil)
	probeState := &managedProbe{}
	probeState.update(ffprobe, decoder, runtimeAudioGate(pluginManager, processor), runtimeAudioGate(pluginManager, decoder))
	probe := library.Probe(probeState.probe)
	lib := library.New(store, queue, runtimes, probe)
	lib.SetProbeSession(probeState.session)
	lib.SetWriteBackProbeReady(probeState.ready)
	content := mediastore.NewContent(store, store, mediaFiles, thumbnail.New())
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
	if err = online.ConfigureAudio(lib); err != nil {
		cleanup()
		return nil, nil, err
	}
	recommendations, err := configureRecommendations(onlineRuntimes, store)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeRecommendations := cleanup
	cleanup = func() { _ = recommendations.Close(); closeBeforeRecommendations() }
	remotePlaylists, err := configureRemotePlaylists(store, online, onlineRuntimes)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	closeBeforeRemotePlaylists := cleanup
	cleanup = func() { remotePlaylists.Close(); closeBeforeRemotePlaylists() }
	sessions := auth.New(store, c.AdminToken, c.AllowedOrigins, time.Duration(c.SessionTTLSeconds)*time.Second)
	availability, hasAvailability := store.(storage.Availability)
	isReady := func() bool {
		return (ready == nil || ready()) && (!hasAvailability || availability.Available())
	}
	handler := api.New(api.Options{Auth: sessions, InstanceID: instance, Ready: isReady, SecureCookie: c.TLSCert != "", Logger: log})
	queue.OnChanged = handler.NotifyScan
	metadata.OnRefreshChanged = handler.NotifyRefreshTask
	handler.RegisterLibrary(lib)
	handler.RegisterCatalog(catalog.New(store))
	handler.RegisterMusicLibrary(store)
	handler.RegisterSourceDisplay(sourcedisplay.New(store, online))
	handler.RegisterPluginManagement(pluginManager)
	handler.RegisterOnline(online)
	handler.RegisterPlaylistTrackSources(online, store)
	handler.RegisterRecommendations(recommendations)
	handler.RegisterRemotePlaylists(remotePlaylists)
	remotePlaylists.OnJobChanged(handler.NotifyPlaylistSync)
	links := playlists.NewLinks(store, online)
	handler.RegisterPlaylistLinks(links)
	handler.RegisterPlaylists(playlists.NewAggregate(store))
	handler.RegisterHashing(hashes)
	handler.RegisterFingerprinting(fingerprints)
	handler.RegisterMedia(content)
	scraperSettings, err := configureOnlineScrapers(handler, store, content)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	handler.RegisterAlbums(albums.New(store, content, lib))
	closeHistory, err := configureWorks(ctx, c, handler, store, lib)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	beforeHistory := cleanup
	cleanup = func() { closeHistory(); beforeHistory() }
	handler.RegisterEnrichment(metadata)
	taskService := tasks.New(store, queue, metadata, lib.TaskNames)
	taskService.OnChanged = handler.NotifyRefreshTask
	beforeTasks := cleanup
	cleanup = func() { taskService.Close(); beforeTasks() }
	handler.RegisterTasks(taskService)
	handler.RegisterMetadataAutomation(metadata)
	handler.RegisterWriteBack(writebacks)
	var fingerprintDetails matching.FingerprintDetails
	if fingerprints != nil {
		fingerprintDetails = fingerprints
	}
	matchingService := matching.New(store, fingerprintDetails, standard.Plugin{})
	handler.RegisterMatching(matchingService)
	matchingAutomation := matching.NewAutomation(store, store, matchingService, standard.Detector())
	matchingAutomation.OnJobChanged(handler.NotifyPluginJobs)
	settingsProviders := append(codecsettings.Providers(), matchingAutomation.Provider())
	settingsProviders = append(settingsProviders, onlinePlaybackSettings()...)
	settingsProviders = append(settingsProviders, scraperSettings...)
	handler.RegisterPluginSettings(settings.New(store, settingsProviders...))
	beforeMatching := cleanup
	cleanup = func() { matchingAutomation.Close(); beforeMatching() }
	player, err := playback.New(ctx, store, lib, processor, playback.Config{CacheRoot: c.CacheRoot, MaxSessions: c.MaxSessions, MaxCacheBytes: c.MaxCacheBytes, Decoder: decoder, RequireDSP: c.AudioDSP.Enabled && len(c.AudioDSP.Chain) > 0 || c.AudioChannels.RequiresProcessing(), DisableEffects: !c.AudioDSP.Enabled, DisableChannelMix: !c.AudioChannels.Enabled})
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
	handler.RegisterDSPSettings(dspsettings.New(store, player.DSPSettingsConfig))
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
	downloads, err := downloadjobs.Open(ctx, filepath.Join(c.DataRoot, "online-download-jobs"), online, player)
	if err != nil {
		cleanup()
		return nil, nil, err
	}
	handler.RegisterOnlineDownloadJobs(downloads)
	beforeDownloads := cleanup
	cleanup = func() { downloads.Close(); beforeDownloads() }
	matchingAutomation.Start(ctx)
	stopLinks := links.Start(ctx)
	beforeLinks := cleanup
	cleanup = func() { stopLinks(); beforeLinks() }
	return handler, cleanup, nil
}
