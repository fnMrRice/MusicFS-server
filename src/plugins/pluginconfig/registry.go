package pluginconfig

import (
	"encoding/json"
	"fmt"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// Declaration 由可信装配代码注册；Worker 的自述不能自行扩大宿主权限。
type Declaration struct {
	ID, Role, SourceType, Platform, Version            string
	SupportsMultipleInstances                          bool
	Directories, AudioTools, AudioProcessing, Settings bool
	Identity                                           *pluginproto.PluginIdentity
	Configuration                                      func() *pluginproto.Configuration
}

var registered = struct {
	sync.RWMutex
	items map[string]Declaration
}{items: map[string]Declaration{}}

func Register(d Declaration) error {
	if d.Identity == nil || d.Identity.Validate(d.ID) != nil || !pluginproto.ValidPluginVersion(d.Version) || (d.Role != "provider" && d.Role != "audio" && d.Role != "scraper") {
		return fmt.Errorf("invalid plugin registration")
	}
	if d.Role == "provider" && d.SourceType != pluginproto.SourceLocal && d.SourceType != pluginproto.SourceNetwork || d.Role != "provider" && d.SourceType != "" {
		return fmt.Errorf("invalid registered source type")
	}
	if d.Directories && (d.Role != "provider" || d.SourceType != pluginproto.SourceLocal || d.Configuration == nil) || d.Platform != "" && (d.Role != "provider" || d.SourceType != pluginproto.SourceNetwork) || (d.AudioTools || d.AudioProcessing) && d.Role != "audio" {
		return fmt.Errorf("invalid registered plugin capabilities")
	}
	if d.Configuration != nil {
		declaration := d.Configuration()
		if declaration == nil || declaration.Validate() != nil {
			return fmt.Errorf("invalid registered import configuration")
		}
		raw, err := json.Marshal(declaration)
		if err != nil {
			return err
		}
		d.Configuration = func() *pluginproto.Configuration {
			var copy pluginproto.Configuration
			_ = json.Unmarshal(raw, &copy)
			return &copy
		}
	}
	d.Identity = d.Identity.Clone()
	registered.Lock()
	defer registered.Unlock()
	if _, ok := registered.items[d.ID]; ok {
		return fmt.Errorf("plugin is already registered")
	}
	registered.items[d.ID] = d
	return nil
}

func Lookup(id string) (Declaration, bool) {
	registered.RLock()
	defer registered.RUnlock()
	d, ok := registered.items[id]
	d.Identity = d.Identity.Clone()
	return d, ok
}
