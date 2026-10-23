package localfiles

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// 身份映射独立于扫描深度和当前可见别名；新增或排除硬链接不改变已知路径 ID。
func (w *worker) identity(ctx context.Context, entry pluginproto.Entry) (string, error) {
	if entry.FileIdentity == "" || entry.RelativePath == "" || entry.LinkCount == 0 {
		return "", fmt.Errorf("RESOURCE_CHANGED: reliable file identity is unavailable")
	}
	w.identityMu.Lock()
	defer w.identityMu.Unlock()
	key := directoryidentity.AliasKey(entry.RootID, entry.FileIdentity)
	var state pluginproto.StateValue
	if err := w.peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: key}, &state); err != nil {
		return "", err
	}
	aliases, err := directoryidentity.DecodeAliases(state)
	if err != nil {
		return "", err
	}
	if id := aliases.Paths[entry.RelativePath]; id != "" {
		return id, nil
	}
	id := ""
	if entry.LinkCount == 1 && len(aliases.Paths) == 1 {
		// 单一历史别名与单硬链接文件才能确定重命名；多个历史别名不能自动合并。
		for oldPath, previous := range aliases.Paths {
			id = previous
			delete(aliases.Paths, oldPath)
		}
	}
	if id == "" {
		if len(aliases.Paths) >= 1024 {
			return "", fmt.Errorf("RATE_LIMITED: source alias limit exceeded")
		}
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return "", err
		}
		id = hex.EncodeToString(random[:])
	}
	aliases.Paths[entry.RelativePath] = id
	value, err := json.Marshal(aliases)
	if err != nil || len(value) > 256<<10 {
		return "", fmt.Errorf("RATE_LIMITED: source identity state is too large")
	}
	if err := w.peer.Call(ctx, "host.state.compareAndSet", pluginproto.StateRequest{Key: key, Revision: state.Revision, Value: value}, nil); err != nil {
		return "", err
	}
	return id, nil
}
