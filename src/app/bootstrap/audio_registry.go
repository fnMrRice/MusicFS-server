package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type AudioInstance struct {
	Processor audioruntime.Processor
	Decoder   audioruntime.Decoder
	Probe     audioruntime.Probe
}
type AudioFactory struct {
	Processing bool
	Create     func(context.Context, config.Config, config.Plugin, config.Instance) (AudioInstance, error)
	Cleanup    func(string) error
}

var audioFactories = struct {
	sync.RWMutex
	items map[string]AudioFactory
}{items: map[string]AudioFactory{}}

// RegisterAudioFactory 在启动前注册能力构造器，配置和生命周期均使用同一入口。
func RegisterAudioFactory(id string, factory AudioFactory) error {
	audioFactories.Lock()
	defer audioFactories.Unlock()
	if id == "" || factory.Create == nil || pluginconfig.Role(id) != "audio" || pluginconfig.ProcessesAudio(id) != factory.Processing {
		return fmt.Errorf("invalid audio factory")
	}
	if _, ok := audioFactories.items[id]; ok {
		return fmt.Errorf("duplicate audio factory")
	}
	audioFactories.items[id] = factory
	return nil
}

func createAudioInstance(ctx context.Context, factory AudioFactory, c config.Config, plugin config.Plugin, instance config.Instance) (AudioInstance, error) {
	value, err := factory.Create(ctx, c, plugin, instance)
	if err != nil {
		return AudioInstance{}, err
	}
	var decoder audioruntime.Decoder = value.Decoder
	if factory.Processing {
		decoder = value.Processor
	}
	if decoder == nil || decoder.AudioDecoder().PluginID != plugin.PluginID {
		return AudioInstance{}, fmt.Errorf("PROTOCOL_MISMATCH: audio factory returned a different plugin identity")
	}
	return value, nil
}
func audioFactory(id string) (AudioFactory, bool) {
	audioFactories.RLock()
	defer audioFactories.RUnlock()
	f, ok := audioFactories.items[id]
	return f, ok
}
func cleanupAudioOutputs(root string) error {
	audioFactories.RLock()
	defer audioFactories.RUnlock()
	ids := make([]string, 0, len(audioFactories.items))
	for id := range audioFactories.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if cleanup := audioFactories.items[id].Cleanup; cleanup != nil {
			if err := cleanup(root); err != nil {
				return err
			}
		}
	}
	return nil
}
