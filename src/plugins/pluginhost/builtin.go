package pluginhost

import (
	"fmt"
	"sync"

	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

const defaultDirectoryPlugin = "musicfs.plugin.source.localfiles"

var directoryRunners = struct {
	sync.RWMutex
	items map[string]inprocess.Runner
}{items: map[string]inprocess.Runner{defaultDirectoryPlugin: localfiles.Run}}

// RegisterBuiltin 注册可信目录型 Provider；目录访问仍通过受限宿主通道执行。
func RegisterBuiltin(id string, run inprocess.Runner) error {
	if run == nil || !pluginconfig.SupportsDirectories(id) {
		return fmt.Errorf("invalid directory provider registration")
	}
	directoryRunners.Lock()
	defer directoryRunners.Unlock()
	if _, ok := directoryRunners.items[id]; ok {
		return fmt.Errorf("directory provider is already registered")
	}
	directoryRunners.items[id] = run
	return nil
}

func directoryRunner(id string) inprocess.Runner {
	directoryRunners.RLock()
	defer directoryRunners.RUnlock()
	return directoryRunners.items[id]
}
