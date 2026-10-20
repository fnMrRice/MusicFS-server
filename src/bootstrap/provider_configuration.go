package bootstrap

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/config"
)

func providerConfigRevision(instance config.Instance, binaryHash string, defaults pluginproto.ImportOptions) string {
	roots := append([]config.MusicRoot(nil), instance.Config.MusicRoots...)
	sort.Slice(roots, func(i, j int) bool { return roots[i].ID < roots[j].ID })
	value := struct {
		InstanceID string                    `json:"instanceId"`
		BinaryHash string                    `json:"binaryHash"`
		Roots      []config.MusicRoot        `json:"roots"`
		Defaults   pluginproto.ImportOptions `json:"defaults"`
	}{instance.InstanceID, binaryHash, roots, defaults}
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(append([]byte("musicfs-provider-config-v1\x00"), raw...))
	return hex.EncodeToString(digest[:])
}
