package enrichment

import (
	"fmt"
	"slices"
	"sync"
)

// HostConfig 只包含实例运行约束；不包含插件业务实现。
type HostConfig struct {
	Builtin                                       bool
	WorkerArgs                                    []string
	WorkerPath, WorkerSHA256, WorkDir, InstanceID string
	MaxConcurrent                                 int
}

type Factory func(HostConfig) (Host, error)

type Registration struct {
	Descriptor Descriptor
	Open       Factory
}

var plugins = struct {
	sync.RWMutex
	values map[string]Registration
}{values: map[string]Registration{}}

// Register 由受信任的编译装配调用；不加载任意外部代码。
func Register(descriptor Descriptor, factory Factory) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if factory == nil {
		return fmt.Errorf("INVALID_ARGUMENT: scraper factory is required")
	}
	plugins.Lock()
	defer plugins.Unlock()
	if _, exists := plugins.values[descriptor.PluginID]; exists {
		return fmt.Errorf("CONFLICT: scraper plugin is already registered")
	}
	descriptor.Fields = slices.Clone(descriptor.Fields)
	plugins.values[descriptor.PluginID] = Registration{Descriptor: descriptor, Open: factory}
	return nil
}

func Lookup(pluginID string) (Registration, bool) {
	plugins.RLock()
	defer plugins.RUnlock()
	entry, ok := plugins.values[pluginID]
	entry.Descriptor.Fields = slices.Clone(entry.Descriptor.Fields)
	return entry, ok
}
