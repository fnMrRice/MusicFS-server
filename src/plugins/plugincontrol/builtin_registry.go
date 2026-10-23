package plugincontrol

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/config"
)

// BuiltinDeclaration 由可信装配声明实际使用的算法，不从名称推断实现或开关。
type BuiltinDeclaration struct {
	PluginID          string
	Identity          *pluginproto.PluginIdentity
	Version           string
	Capabilities      []string
	SettingsAvailable bool
	Configured        func(config.Config) bool
}

func (d BuiltinDeclaration) clone() BuiltinDeclaration {
	d.Identity = d.Identity.Clone()
	d.Capabilities = append([]string(nil), d.Capabilities...)
	return d
}

func (d BuiltinDeclaration) validate() error {
	if d.Identity == nil || d.Identity.Validate(d.PluginID) != nil || !pluginproto.ValidPluginVersion(d.Version) || d.Configured == nil || len(d.Capabilities) == 0 {
		return fmt.Errorf("invalid builtin algorithm declaration")
	}
	seen := map[string]bool{}
	for _, capability := range d.Capabilities {
		if capability == "" || len(capability) > 128 || strings.ContainsAny(capability, "\x00\r\n") || seen[capability] {
			return fmt.Errorf("invalid builtin algorithm capabilities")
		}
		seen[capability] = true
	}
	return nil
}

var builtinRegistry = struct {
	sync.RWMutex
	items map[string]BuiltinDeclaration
}{items: map[string]BuiltinDeclaration{}}

// RegisterBuiltin 只增加可信装配声明，不覆盖已有插件身份。
func RegisterBuiltin(d BuiltinDeclaration) error {
	if err := d.validate(); err != nil {
		return err
	}
	builtinRegistry.Lock()
	defer builtinRegistry.Unlock()
	if _, exists := builtinRegistry.items[d.PluginID]; exists {
		return fmt.Errorf("builtin algorithm is already registered")
	}
	builtinRegistry.items[d.PluginID] = d.clone()
	return nil
}

// BuiltinCatalog 返回快照；管理器构造后不受全局注册目录变更影响。
func BuiltinCatalog() []BuiltinDeclaration {
	builtinRegistry.RLock()
	defer builtinRegistry.RUnlock()
	items := make([]BuiltinDeclaration, 0, len(builtinRegistry.items))
	for _, d := range builtinRegistry.items {
		items = append(items, d.clone())
	}
	sort.Slice(items, func(i, j int) bool { return items[i].PluginID < items[j].PluginID })
	return items
}
