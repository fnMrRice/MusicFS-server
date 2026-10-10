package bootstrap

import (
	"context"
	"log/slog"

	"github.com/fnMrRice/MusicFS-server/src/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
)

func audioGate(manager *plugincontrol.Manager, pluginID string) *plugincontrol.Gate {
	c := manager.RuntimeConfig()
	for _, p := range c.Plugins {
		if p.PluginID == pluginID {
			for _, i := range p.Instances {
				if p.CanStart(i) {
					return manager.Gate(i.InstanceID)
				}
			}
		}
	}
	return nil
}

func backgroundAudioUpdate(manager *plugincontrol.Manager, probe *managedProbe, metadata *enrichmentworker.Service, fingerprints *fingerprintworker.Service, log *slog.Logger) func(context.Context, *audioffmpeg.Processor, *ncmdecoder.Processor, string) error {
	return func(ctx context.Context, audio *audioffmpeg.Processor, ncm *ncmdecoder.Processor, ffprobe string) error {
		c := manager.RuntimeConfig()
		var metadataDecoder, fingerprintDecoder *ncmdecoder.Processor
		if ncm != nil {
			metadataDecoder = fingerprintNCMDecoder(c, log)
			fingerprintDecoder = fingerprintNCMDecoder(c, log)
			if metadataDecoder != nil {
				metadataDecoder.SetGate(audioGate(manager, "musicfs.ncm"))
			}
			if fingerprintDecoder != nil {
				fingerprintDecoder.SetGate(audioGate(manager, "musicfs.ncm"))
			}
		}
		if fingerprints != nil {
			extractor, err := createFingerprintExtractor(ctx, c, audio)
			if err != nil {
				log.Warn("fingerprinting dependency unavailable", "errorCode", "UNSUPPORTED")
			}
			fingerprints.UpdateRuntime(extractor, fingerprintDecoder, audioGate(manager, "musicfs.audio.ffmpeg"))
		}
		metadata.UpdateDecoder(metadataDecoder)
		probe.update(ffprobe, ncm, audioGate(manager, "musicfs.audio.ffmpeg"), audioGate(manager, "musicfs.ncm"))
		return nil
	}
}
