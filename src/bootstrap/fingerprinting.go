package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func configureFingerprinting(ctx context.Context, c config.Config, log *slog.Logger, store *sqlite.Store, lib *library.Service, audio *audioffmpeg.Processor) (*fingerprintworker.Service, func()) {
	noop := func() {}
	if !c.Fingerprinting.Enabled {
		return nil, noop
	}
	if audio == nil || !audio.Capabilities.Normalization {
		log.Warn("fingerprinting unavailable", "errorCode", "UNSUPPORTED")
		return nil, noop
	}
	f := c.Fingerprinting
	extractor, err := fingerprint.New(ctx, fingerprint.Config{FpcalcPath: f.FpcalcPath, FpcalcSHA256: f.FpcalcSHA256, FFmpegSHA256: audio.Capabilities.FFmpegSHA256, FFmpegVersion: audio.Capabilities.Version, MaxConcurrent: f.MaxConcurrent, Timeout: time.Duration(f.TimeoutSeconds) * time.Second}, audio)
	if err != nil {
		log.Warn("fingerprinting unavailable", "errorCode", pluginproto.ErrorCode(err))
		return nil, noop
	}
	files, err := mediastore.OpenFiles(c.MediaRoot)
	if err != nil {
		log.Warn("fingerprint media store unavailable", "errorCode", "TEMPORARY_FAILURE")
		return nil, noop
	}
	media, err := mediastore.New(store, files)
	if err != nil {
		files.Close()
		log.Warn("fingerprint media store unavailable", "errorCode", pluginproto.ErrorCode(err))
		return nil, noop
	}
	worker, err := fingerprintworker.New(store, lib, extractor, media, fingerprintworker.Config{MaxConcurrent: f.MaxConcurrent, Timeout: time.Duration(f.TimeoutSeconds) * time.Second, NCMDecoder: fingerprintNCMDecoder(c, log)})
	if err == nil {
		err = worker.Start(ctx)
	}
	if err != nil {
		if worker != nil {
			worker.Close()
		}
		files.Close()
		log.Warn("fingerprinting unavailable", "errorCode", pluginproto.ErrorCode(err))
		return nil, noop
	}
	return worker, func() { worker.Close(); files.Close() }
}

// Background decryption has a separate bounded lane so fingerprint jobs cannot
// occupy the only foreground NCM playback slot when maxProcessing is one.
func fingerprintNCMDecoder(c config.Config, log *slog.Logger) *ncmdecoder.Processor {
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.ncm" || !plugin.Enabled {
			continue
		}
		for _, instance := range plugin.Instances {
			if !instance.Enabled || instance.Role != "audio" {
				continue
			}
			decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{WorkerPath: plugin.Executable, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, MaxConcurrent: 1})
			if err != nil {
				log.Warn("fingerprint NCM decoding unavailable", "errorCode", pluginproto.ErrorCode(err))
				return nil
			}
			return decoder
		}
	}
	return nil
}
