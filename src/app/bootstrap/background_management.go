package bootstrap

import (
	"context"
	"log/slog"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
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

func runtimeAudioGate(manager *plugincontrol.Manager, decoder audioruntime.Decoder) *plugincontrol.Gate {
	if decoder == nil {
		return nil
	}
	return audioGate(manager, decoder.AudioDecoder().PluginID)
}

func backgroundAudioUpdate(manager *plugincontrol.Manager, probe *managedProbe, metadata *enrichmentworker.Service, fingerprints *fingerprintworker.Service, log *slog.Logger) func(context.Context, audioruntime.Processor, audioruntime.Decoder, audioruntime.Probe) error {
	return func(ctx context.Context, audio audioruntime.Processor, ncm audioruntime.Decoder, ffprobe audioruntime.Probe) error {
		c := manager.RuntimeConfig()
		var metadataDecoder, fingerprintDecoder audioruntime.Decoder
		if ncm != nil {
			metadataDecoder = backgroundDecoder(c, log)
			fingerprintDecoder = backgroundDecoder(c, log)
			if metadataDecoder != nil {
				metadataDecoder.SetGate(audioGate(manager, metadataDecoder.AudioDecoder().PluginID))
			}
			if fingerprintDecoder != nil {
				fingerprintDecoder.SetGate(audioGate(manager, fingerprintDecoder.AudioDecoder().PluginID))
			}
		}
		if fingerprints != nil {
			extractor, err := createFingerprintExtractor(ctx, c, audio)
			if err != nil {
				log.Warn("fingerprinting dependency unavailable", "errorCode", "UNSUPPORTED")
			}
			fingerprints.UpdateRuntime(extractor, fingerprintDecoder, audioGate(manager, audioruntime.Safe(audio).AudioDecoder().PluginID))
		}
		metadata.UpdateDecoder(metadataDecoder)
		decoderID := ""
		if ncm != nil {
			decoderID = ncm.AudioDecoder().PluginID
		}
		probe.update(ffprobe, ncm, audioGate(manager, audioruntime.Safe(audio).AudioDecoder().PluginID), audioGate(manager, decoderID))
		return nil
	}
}
