// Package audiocodecs 组合已安装编解码插件的识别能力，不包含任何文件格式规则。
package audiocodecs

import (
	"fmt"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"io"
	"sort"
	"sync"
)

var registrations = struct {
	sync.RWMutex
	decoders   map[string]audioformat.Decoder
	inspectors map[string]func(string, int64) (audiotypes.Metadata, error)
}{decoders: map[string]audioformat.Decoder{}, inspectors: map[string]func(string, int64) (audiotypes.Metadata, error){}}

func Register(decoder audioformat.Decoder, inspectors map[string]func(string, int64) (audiotypes.Metadata, error)) error {
	if _, err := audioformat.NewChain(decoder); err != nil {
		return err
	}
	registrations.Lock()
	defer registrations.Unlock()
	if _, ok := registrations.decoders[decoder.PluginID]; ok {
		return fmt.Errorf("duplicate audio decoder registration")
	}
	for mime, inspect := range inspectors {
		if mime == "" || inspect == nil || registrations.inspectors[mime] != nil {
			return fmt.Errorf("invalid audio inspector registration")
		}
	}
	registrations.decoders[decoder.PluginID] = decoder
	for mime, inspect := range inspectors {
		registrations.inspectors[mime] = inspect
	}
	return nil
}

// DefaultRegistry 共享官方插件声明的只读注册表，包括未启用解码实例的格式识别。
// 识别成功不代表解码实例已启用；播放和分析仍须经过实例生命周期授权。
func DefaultRegistry() *audioformat.Registry {
	values := registeredDecoders()
	ids := make([]string, 0, len(values))
	for id := range values {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	detectors := make([]audioformat.Detector, 0, len(ids))
	for _, id := range ids {
		detectors = append(detectors, values[id].Detector)
	}
	registry, err := audioformat.New(detectors...)
	if err != nil {
		panic(err)
	}
	return registry
}

func registeredDecoders() map[string]audioformat.Decoder {
	registrations.RLock()
	defer registrations.RUnlock()
	result := make(map[string]audioformat.Decoder, len(registrations.decoders))
	for id, d := range registrations.decoders {
		result[id] = d
	}
	return result
}

func InspectNative(mime, path string, size int64) (audiotypes.Metadata, error) {
	registrations.RLock()
	inspect := registrations.inspectors[mime]
	registrations.RUnlock()
	if inspect == nil {
		return audiotypes.Metadata{}, audioformat.ErrUnsupported
	}
	return inspect(path, size)
}

func Detect(input io.ReaderAt, size int64) (audioformat.Match, bool, error) {
	return DefaultRegistry().Detect(input, size)
}
