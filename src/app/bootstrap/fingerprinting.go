package bootstrap

import (
	"context"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/analysis/chromaprint"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func configureFingerprinting(ctx context.Context, c config.Config, log *slog.Logger, store storage.Backend, lib *library.Service, audio audioruntime.Processor, media *mediastore.Service) (*fingerprintworker.Service, func()) {
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
		worker.UpdateRuntime(extractor, backgroundDecoder(c, log), nil)
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

func createFingerprintExtractor(ctx context.Context, c config.Config, audio audioruntime.Processor) (fingerprintworker.Extractor, error) {
	if audio == nil || !audio.RuntimeCapabilities().Normalization {
		return nil, nil
	}
	f := c.Fingerprinting
	capabilities := audio.RuntimeCapabilities()
	extractor, err := chromaprint.New(ctx, chromaprint.Config{FpcalcPath: f.FpcalcPath, FpcalcSHA256: f.FpcalcSHA256, DecoderSHA256: capabilities.DecoderSHA256, DecoderVersion: capabilities.Version, MaxConcurrent: f.MaxConcurrent, Timeout: time.Duration(f.TimeoutSeconds) * time.Second}, audio)
	if err != nil {
		return nil, err
	}
	return extractor, nil
}

// 后台解封装使用独立并发配额，不占用前台播放的处理通道。
func backgroundDecoder(c config.Config, log *slog.Logger) audioruntime.Decoder {
	c.MaxProcessing = 1
	for _, plugin := range c.Plugins {
		factory, registered := audioFactory(plugin.PluginID)
		if !registered || factory.Processing || !plugin.Enabled {
			continue
		}
		for _, instance := range plugin.Instances {
			if !plugin.CanStart(instance) {
				continue
			}
			value, err := createAudioInstance(context.Background(), factory, c, plugin, instance)
			if err != nil {
				log.Warn("background audio decoding unavailable", "errorCode", pluginproto.ErrorCode(err))
				return nil
			}
			return value.Decoder
		}
	}
	return nil
}
