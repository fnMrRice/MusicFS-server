package plugindb

import (
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginid"
)

// ValidateScope 验证私有状态的当前插件标识和实例 UUID，供两个后端和备份共用。
func ValidateScope(plugin, instance string) error {
	if plugin == "" || strings.ContainsRune(plugin, 0) || pluginid.Canonical(plugin) != plugin || !instanceid.Valid(instance) {
		return domain.Fail("ACCESS_DENIED", "Invalid plugin database scope")
	}
	return nil
}
