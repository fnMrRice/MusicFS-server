package bootstrap

import (
	"context"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

// configureAudioManagement rebuilds verified processors only after the manager
// has drained their previous leases. onChange refreshes background consumers
// before the new generation can accept work; long-running work must use runCtx.
func configureAudioManagement(c config.Config, manager *plugincontrol.Manager, player *playback.Service, processor *audioffmpeg.Processor, decoder *ncmdecoder.Processor, onChange func(context.Context, *audioffmpeg.Processor, *ncmdecoder.Processor, string) error) error {
	var mu sync.Mutex
	var changeMu sync.Mutex
	currentAudio, currentDecoder := processor, decoder
	probePath := ""
	for _, plugin := range c.Plugins {
		if plugin.PluginID == "musicfs.plugin.decoder.ffmpeg" && plugin.Enabled && processor != nil {
			for _, instance := range plugin.Instances {
				if instance.Enabled {
					probePath = instance.Config.FFprobePath
				}
			}
		}
	}
	apply := func(ctx context.Context, audio *audioffmpeg.Processor, ncm *ncmdecoder.Processor, probe string) error {
		if onChange != nil {
			if err := onChange(ctx, audio, ncm, probe); err != nil {
				return err
			}
		}
		player.UpdateAudio(audio, ncm)
		mu.Lock()
		currentAudio, currentDecoder, probePath = audio, ncm, probe
		mu.Unlock()
		return nil
	}
	for _, plugin := range c.Plugins {
		if plugin.PluginID != "musicfs.plugin.decoder.ffmpeg" && plugin.PluginID != "musicfs.plugin.decoder.ncm" {
			continue
		}
		for _, instance := range plugin.Instances {
			gate := manager.Gate(instance.InstanceID)
			isAudio := plugin.PluginID == "musicfs.plugin.decoder.ffmpeg"
			ready := plugin.CanStart(instance) && (isAudio && processor != nil || !isAudio && decoder != nil)
			if ready {
				if isAudio {
					processor.SetGate(gate)
				} else {
					decoder.SetGate(gate)
				}
			}
			hooks := plugincontrol.Hooks{
				Ready: func() bool { mu.Lock(); defer mu.Unlock(); return ready },
				DSPVersion: func(name string) string {
					mu.Lock()
					defer mu.Unlock()
					if isAudio && currentAudio != nil {
						return currentAudio.DSPPluginVersion(name)
					}
					return ""
				},
				PluginVersion: func() string {
					mu.Lock()
					defer mu.Unlock()
					if isAudio && currentAudio != nil {
						return currentAudio.PluginVersion()
					}
					if !isAudio && currentDecoder != nil {
						return currentDecoder.PluginVersion()
					}
					return ""
				},
				Capabilities: func() []string {
					mu.Lock()
					defer mu.Unlock()
					if !ready {
						return []string{}
					}
					if !isAudio {
						return []string{"audio.decrypt"}
					}
					caps := []string{"audio.decode", "audio.transcode", "audio.dsp", "audio.encode", "audio.mux"}
					if currentAudio != nil && currentAudio.Capabilities.Normalization {
						caps = append(caps, "audio.normalize")
					}
					return caps
				},
				Start: func(ctx context.Context, p config.Plugin, i config.Instance) error {
					changeMu.Lock()
					defer changeMu.Unlock()
					mu.Lock()
					activeAudio, activeDecoder, activeProbe := currentAudio, currentDecoder, probePath
					mu.Unlock()
					if isAudio {
						audio, err := audioffmpeg.NewProcessor(ctx, audioffmpeg.Config{Builtin: p.InProcess(), InstanceID: i.InstanceID, FFmpegPath: i.Config.FFmpegPath, FFprobePath: i.Config.FFprobePath, WorkerPath: p.Executable, WorkerArgs: p.Args, WorkerSHA256: pluginChecksum(p), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing, DSPChain: c.AudioDSP.Chain, DSPPlugins: c.AudioDSP.Plugins, OutputLayout: c.AudioChannels.OutputLayout, DisableEffects: !c.AudioDSP.Enabled, DisableChannelMix: !c.AudioChannels.Enabled})
						if err != nil {
							return err
						}
						audio.SetGate(gate)
						if err = apply(ctx, audio, activeDecoder, i.Config.FFprobePath); err != nil {
							return err
						}
					} else {
						ncm, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: p.InProcess(), InstanceID: i.InstanceID, WorkerPath: p.Executable, WorkerArgs: p.Args, WorkerSHA256: pluginChecksum(p), WorkDir: c.DataRoot, MaxConcurrent: c.MaxProcessing})
						if err != nil {
							return err
						}
						ncm.SetGate(gate)
						if err = apply(ctx, activeAudio, ncm, activeProbe); err != nil {
							return err
						}
					}
					mu.Lock()
					ready = true
					mu.Unlock()
					return nil
				},
				Stop: func(ctx context.Context) error {
					changeMu.Lock()
					defer changeMu.Unlock()
					for gate.Active() != 0 {
						select {
						case <-ctx.Done():
							return ctx.Err()
						case <-time.After(10 * time.Millisecond):
						}
					}
					mu.Lock()
					audio, ncm, probe := currentAudio, currentDecoder, probePath
					mu.Unlock()
					if isAudio {
						audio, probe = nil, ""
					} else {
						ncm = nil
					}
					if err := apply(ctx, audio, ncm, probe); err != nil {
						return err
					}
					mu.Lock()
					ready = false
					mu.Unlock()
					return nil
				},
			}
			if err := manager.Register(instance.InstanceID, hooks, ready); err != nil {
				return err
			}
		}
	}
	return nil
}
