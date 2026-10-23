// Package directoryidentity 定义目录 Provider 可选的稳定路径身份协议。
// 声明 Capability 的插件使用这些状态键和结构，允许宿主在文件原子替换后
// 通过持久化日志重绑身份；未采用此协议的插件不能请求此能力。
package directoryidentity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

const Capability = "library.rebind.stable-paths-v1"

// Aliases 保存一个文件身份在同一授权根内已观察到的路径及稳定来源 ID。
type Aliases struct {
	Paths map[string]string `json:"paths"`
}

var externalID = regexp.MustCompile(`^[a-z0-9_-]{1,74}$`)

// ValidID 保证来源 ID 可无歧义地用于协议状态键，不限定插件的生成算法。
func ValidID(id string) bool { return externalID.MatchString(id) }

func AliasKey(root, identity string) string {
	h := sha256.Sum256([]byte(root + ":" + identity))
	return "aliases-" + hex.EncodeToString(h[:])
}

func TrackKey(id string) string { return "track-" + id }

func DecodeAliases(state pluginproto.StateValue) (Aliases, error) {
	a := Aliases{Paths: map[string]string{}}
	if len(state.Value) > 0 && (json.Unmarshal(state.Value, &a) != nil || a.Paths == nil || len(a.Paths) > 1024) {
		return a, fmt.Errorf("RESOURCE_CHANGED: invalid directory identity mapping")
	}
	for _, id := range a.Paths {
		if !ValidID(id) {
			return a, fmt.Errorf("RESOURCE_CHANGED: invalid directory source identity")
		}
	}
	return a, nil
}
