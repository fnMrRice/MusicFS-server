package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func configureFingerprinting(ctx context.Context, c config.Config, log *slog.Logger, store storage.Backend, lib *library.Service, audio *audioffmpeg.Processor, media *mediastore.Service) (*fingerprintworker.Service, func()) {
	noop := func() {}
	if !c.Fingerprinting.Enabled {
		return nil, noop
	}
	f := c.Fingerprinting
	extractor, err := createFingerprintExtractor(ctx, c, audio)
	if err != nil {
		log.Warn("fingerprinting unavailable", "errorCode", pluginproto.ErrorCode(err))
	}
	worker, err := fingerprintworker.NewDormant(store, lib, media, fingerprintworker.Config{MaxConcurrent: f.MaxConcurrent, Timeout: time.Duration(f.TimeoutSeconds) * time.Second})
	if err == nil {
		worker.UpdateRuntime(extractor, fingerprintNCMDecoder(c, log), nil)
	}
	if err != nil {
		if worker != nil {
			worker.Close()
		}
		log.Warn("fingerprinting unavailable", "errorCode", pluginproto.ErrorCode(err))
		return nil, noop
	}
	return worker, worker.Close
}

func createFingerprintExtractor(ctx context.Context, c config.Config, audio *audioffmpeg.Processor) (fingerprintworker.Extractor, error) {
	if audio == nil || !audio.Capabilities.Normalization {
		return nil, nil
	}
	f := c.Fingerprinting
	extractor, err := fingerprint.New(ctx, fingerprint.Config{FpcalcPath: f.FpcalcPath, FpcalcSHA256: f.FpcalcSHA256, FFmpegSHA256: audio.Capabilities.FFmpegSHA256, FFmpegVersion: audio.Capabilities.Version, MaxConcurrent: f.MaxConcurrent, Timeout: time.Duration(f.TimeoutSeconds) * time.Second}, audio)
	if err != nil {
		return nil, err
	}
	return extractor, nil
}

// Background decryption has a separate bounded lane so fingerprint jobs cannot
// occupy the only foreground NCM playback slot when maxProcessing is one.
func fingerprintNCMDecoder(c config.Config, log *slog.Logger) *ncmdecoder.Processor {
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.plugin.decoder.ncm" || !plugin.Enabled {
			continue
		}
		for _, instance := range plugin.Instances {
			if !instance.Enabled {
				continue
			}
			decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{InstanceID: instance.InstanceID, WorkerPath: plugin.Executable, WorkerArgs: plugin.Args, WorkerSHA256: expectedChecksum(plugin.SHA256, plugin.Executable), WorkDir: c.DataRoot, MaxConcurrent: 1})
			if err != nil {
				log.Warn("fingerprint NCM decoding unavailable", "errorCode", pluginproto.ErrorCode(err))
				return nil
			}
			return decoder
		}
	}
	return nil
}
