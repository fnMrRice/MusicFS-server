package audioruntime

import (
	"fmt"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
)

var profiles = struct {
	sync.RWMutex
	lookups map[string]func(string) (audiotypes.Profile, bool)
}{lookups: map[string]func(string) (audiotypes.Profile, bool){}}

// RegisterProfiles 由插件装配提供稳定的输出身份解析，不依赖运行实例在线。
func RegisterProfiles(id string, lookup func(string) (audiotypes.Profile, bool)) error {
	profiles.Lock()
	defer profiles.Unlock()
	if id == "" || lookup == nil || profiles.lookups[id] != nil {
		return fmt.Errorf("invalid or duplicate audio profile registration")
	}
	profiles.lookups[id] = lookup
	return nil
}
func LookupProfile(id string) (audiotypes.Profile, bool) {
	profiles.RLock()
	defer profiles.RUnlock()
	var result audiotypes.Profile
	found := false
	for _, lookup := range profiles.lookups {
		if p, ok := lookup(id); ok {
			if found && p != result {
				return audiotypes.Profile{}, false
			}
			result, found = p, true
		}
	}
	return result, found
}
