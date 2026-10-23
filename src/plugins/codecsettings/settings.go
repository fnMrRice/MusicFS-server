// Package codecsettings 将编解码插件声明接入宿主的每用户设置存储。
package codecsettings

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

const FFmpegID = "musicfs.plugin.decoder.ffmpeg"
const NCMID = "musicfs.plugin.decoder.ncm"

type declaration struct {
	provider    settings.Provider
	outputField string
}

var registry = struct {
	sync.RWMutex
	items map[string]declaration
}{items: map[string]declaration{}}

// Register 接收插件设置声明及播放输出偏好字段，设置服务不认识具体插件。
func Register(provider settings.Provider, outputField string) error {
	registry.Lock()
	defer registry.Unlock()
	if provider.ID == "" {
		return fmt.Errorf("invalid codec settings registration")
	}
	if _, ok := registry.items[provider.ID]; ok {
		return fmt.Errorf("duplicate codec settings registration")
	}
	if outputField != "" {
		if _, ok := settings.Defaults(provider.Schema)[outputField].(string); !ok {
			return fmt.Errorf("invalid codec output settings field")
		}
	}
	registry.items[provider.ID] = declaration{provider, outputField}
	return nil
}

func Providers() []settings.Provider {
	registry.RLock()
	defer registry.RUnlock()
	ids := make([]string, 0, len(registry.items))
	for id := range registry.items {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]settings.Provider, 0, len(ids))
	for _, id := range ids {
		result = append(result, registry.items[id].provider)
	}
	return result
}

// DecodeOutput 读取已保存的格式；老存储实现仍使用插件默认值。
func DecodeOutput(ctx context.Context, repository any, owner string, pluginIDs ...string) (string, error) {
	registry.RLock()
	var selected declaration
	if len(pluginIDs) > 0 {
		selected = registry.items[pluginIDs[0]]
	} else {
		for _, entry := range registry.items {
			if entry.outputField != "" {
				if selected.outputField != "" {
					registry.RUnlock()
					return "", fmt.Errorf("audio output settings require an explicit plugin")
				}
				selected = entry
			}
		}
	}
	registry.RUnlock()
	if selected.outputField == "" {
		return "native", nil
	}
	repo, ok := repository.(settings.Repository)
	if !ok {
		return "native", nil
	}
	record, err := repo.GetPluginSettings(ctx, owner, selected.provider.ID)
	if err != nil {
		return "", err
	}
	values := record.Values
	if values == nil {
		values = settings.Defaults(selected.provider.Schema)
	}
	values, err = settings.Normalize(selected.provider.Schema, values)
	if err != nil {
		return "", err
	}
	return values[selected.outputField].(string), nil
}
