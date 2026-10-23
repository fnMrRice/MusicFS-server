package bootstrap

import (
	"context"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

// configureAudioManagement rebuilds verified processors only after the manager
// has drained their previous leases. onChange refreshes background consumers
// before the new generation can accept work; long-running work must use runCtx.
func configureAudioManagement(c config.Config, manager *plugincontrol.Manager, player *playback.Service, processor audioruntime.Processor, decoder audioruntime.Decoder, onChange func(context.Context, audioruntime.Processor, audioruntime.Decoder, audioruntime.Probe) error) error {
	var mu sync.Mutex
	var changeMu sync.Mutex
	currentAudio, currentDecoder := processor, decoder
	var probePath audioruntime.Probe
	if processor != nil && processor.Available() {
		probePath = processor.ProbeOutputMetadata
	}
	apply := func(ctx context.Context, audio audioruntime.Processor, ncm audioruntime.Decoder, probe audioruntime.Probe) error {
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
		factory, registered := audioFactory(plugin.PluginID)
		if !registered {
			continue
		}
		for _, instance := range plugin.Instances {
			gate := manager.Gate(instance.InstanceID)
			isAudio := factory.Processing
			ready := plugin.CanStart(instance) && (isAudio && processor != nil && processor.AudioDecoder().PluginID == plugin.PluginID || !isAudio && decoder != nil && decoder.AudioDecoder().PluginID == plugin.PluginID)
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
					if isAudio && currentAudio != nil && currentAudio.AudioDecoder().PluginID == plugin.PluginID {
						return currentAudio.DSPPluginVersion(name)
					}
					return ""
				},
				PluginVersion: func() string {
					mu.Lock()
					defer mu.Unlock()
					if isAudio && currentAudio != nil && currentAudio.AudioDecoder().PluginID == plugin.PluginID {
						return currentAudio.PluginVersion()
					}
					if !isAudio && currentDecoder != nil && currentDecoder.AudioDecoder().PluginID == plugin.PluginID {
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
					if currentAudio != nil && currentAudio.RuntimeCapabilities().Normalization {
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
						value, err := createAudioInstance(ctx, factory, c, p, i)
						if err != nil {
							return err
						}
						value.Processor.SetGate(gate)
						if err = apply(ctx, value.Processor, activeDecoder, value.Probe); err != nil {
							return err
						}
					} else {
						value, err := createAudioInstance(ctx, factory, c, p, i)
						if err != nil {
							return err
						}
						value.Decoder.SetGate(gate)
						if err = apply(ctx, activeAudio, value.Decoder, activeProbe); err != nil {
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
					if isAudio && audio != nil && audio.AudioDecoder().PluginID == plugin.PluginID {
						audio, probe = nil, nil
					} else if !isAudio && ncm != nil && ncm.AudioDecoder().PluginID == plugin.PluginID {
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
