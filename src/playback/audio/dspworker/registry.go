package dspworker

import (
	"fmt"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type builtinDefinition struct {
	version string
	factory audio.Factory
}

var builtins = struct {
	sync.RWMutex
	values map[string]builtinDefinition
}{values: map[string]builtinDefinition{}}

// RegisterBuiltin 由可信装配注册内嵌协议实例，通用传输层不选择具体算法。
func RegisterBuiltin(name, version string, factory audio.Factory) error {
	builtins.Lock()
	defer builtins.Unlock()
	if !validName.MatchString(name) || !pluginproto.ValidPluginVersion(version) || factory == nil {
		return fmt.Errorf("invalid builtin DSP definition")
	}
	if _, ok := builtins.values[name]; ok {
		return fmt.Errorf("duplicate builtin DSP definition")
	}
	builtins.values[name] = builtinDefinition{version, factory}
	return nil
}
func builtinFor(name string) (builtinDefinition, bool) {
	builtins.RLock()
	defer builtins.RUnlock()
	value, ok := builtins.values[name]
	return value, ok
}
