package onlineplugins

import (
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/applemusic"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/bilibili"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/netease"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/qqmusic"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

// PluginFactory 只能由受信任启动代码注册，RPC 和用户配置不能注入工厂。
type PluginFactory func(Options) provider.Plugin

var factories = struct {
	sync.RWMutex
	values map[string]PluginFactory
}{values: map[string]PluginFactory{
	online.Netease: netease.Plugin, online.QQMusic: qqmusic.Plugin,
	online.AppleMusic: applemusic.Plugin, online.Bilibili: bilibili.Plugin,
}}

// Register 接入独立平台实现；宿主仍需单独注册插件准入声明。
func Register(platform string, factory PluginFactory) error {
	if factory == nil {
		return fmt.Errorf("online plugin factory is required")
	}
	plugin := factory(Options{})
	if plugin.Descriptor.Platform != platform || plugin.Validate() != nil {
		return fmt.Errorf("invalid online plugin registration")
	}
	factories.Lock()
	defer factories.Unlock()
	if _, exists := factories.values[platform]; exists {
		return fmt.Errorf("online platform is already registered")
	}
	factories.values[platform] = factory
	return nil
}

// Plugin 在打开协议通道前选择可信工厂，并隔离调用者持有的声明。
func Plugin(platform string, options Options) provider.Plugin {
	factories.RLock()
	factory := factories.values[platform]
	factories.RUnlock()
	if factory == nil {
		return provider.Plugin{}
	}
	plugin := factory(options)
	plugin.Descriptor = plugin.Descriptor.Clone()
	plugin.Environment = slices.Clone(plugin.Environment)
	plugin.PlaybackSettings = clonePlaybackSettings(plugin.PlaybackSettings)
	return plugin
}

func clonePlaybackSettings(source []pluginsettings.Field) []pluginsettings.Field {
	fields := slices.Clone(source)
	for i := range fields {
		f := &fields[i]
		f.LocalizedLabels = maps.Clone(f.LocalizedLabels)
		f.LocalizedDescriptions = maps.Clone(f.LocalizedDescriptions)
		f.Options = slices.Clone(f.Options)
		for j := range f.Options {
			f.Options[j].LocalizedLabels = maps.Clone(f.Options[j].LocalizedLabels)
		}
		if f.Min != nil {
			f.Min = new(*f.Min)
		}
		if f.Max != nil {
			f.Max = new(*f.Max)
		}
	}
	return fields
}

// Descriptors 返回已装配插件的声明，供受信任宿主建立准入注册表。
func Descriptors() []pluginproto.OnlineDescriptor {
	factories.RLock()
	platforms := make([]string, 0, len(factories.values))
	for platform := range factories.values {
		platforms = append(platforms, platform)
	}
	factories.RUnlock()
	slices.Sort(platforms)
	result := make([]pluginproto.OnlineDescriptor, 0, len(platforms))
	for _, platform := range platforms {
		result = append(result, Describe(platform))
	}
	return result
}
