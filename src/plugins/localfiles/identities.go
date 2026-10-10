package localfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type identityAliases struct {
	Paths map[string]string `json:"paths"`
}

// The mapping is independent of a scan's depth and currently visible aliases.
// Known paths retain their own IDs even when a hard link is added or excluded.
func (w *worker) identity(ctx context.Context, entry pluginproto.Entry) (string, error) {
	if entry.FileIdentity == "" || entry.RelativePath == "" || entry.LinkCount == 0 {
		return "", fmt.Errorf("RESOURCE_CHANGED: reliable file identity is unavailable")
	}
	w.identityMu.Lock()
	defer w.identityMu.Unlock()
	keyBytes := sha256.Sum256([]byte(entry.RootID + ":" + entry.FileIdentity))
	key := "aliases-" + hex.EncodeToString(keyBytes[:])
	var state pluginproto.StateValue
	if err := w.peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: key}, &state); err != nil {
		return "", err
	}
	aliases := identityAliases{Paths: map[string]string{}}
	if len(state.Value) > 0 {
		if err := json.Unmarshal(state.Value, &aliases); err != nil || aliases.Paths == nil {
			return "", fmt.Errorf("RESOURCE_CHANGED: invalid source identity state")
		}
	}
	if id := aliases.Paths[entry.RelativePath]; id != "" {
		return id, nil
	}
	id := ""
	if entry.LinkCount == 1 && len(aliases.Paths) == 1 {
		// A sole observed alias and an actual single-link inode make a rename
		// unambiguous. Multiple historical aliases never silently collapse.
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
