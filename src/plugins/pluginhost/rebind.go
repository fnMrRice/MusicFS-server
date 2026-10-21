package pluginhost

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// LocalRebind is a trusted host operation, not a plugin RPC or public request.
// The caller must retain its provider-wide exclusive writeback lease.
type LocalRebind struct {
	JobID        string `json:"jobId"`
	RootID       string `json:"rootId"`
	RootIdentity string `json:"rootIdentity"`
	RelativePath string `json:"relativePath"`
	ExternalID   string `json:"externalId"`
	OldIdentity  string `json:"oldIdentity"`
	OldVersion   string `json:"oldVersion"`
	NewIdentity  string `json:"newIdentity"`
	NewVersion   string `json:"newVersion"`
}
type localAliases struct {
	Paths map[string]string `json:"paths"`
}
type rebindChange struct {
	Key           string `json:"key"`
	Before, After pluginproto.StateValue
}
type rebindIntent struct {
	Request     LocalRebind             `json:"request"`
	Observation pluginproto.Observation `json:"observation"`
	Changes     []rebindChange          `json:"changes"`
	Complete    bool                    `json:"complete"`
}

const rebindKey = "host-rebind-intent"

var localExternalID = regexp.MustCompile(`^[0-9a-f]{32}$`)
var localJobID = regexp.MustCompile(`^[0-9a-f]{48}$`)

func aliasKey(root, identity string) string {
	h := sha256.Sum256([]byte(root + ":" + identity))
	return "aliases-" + hex.EncodeToString(h[:])
}

func (w *Worker) RebindLocalTrack(ctx context.Context, r LocalRebind) (pluginproto.Observation, error) {
	if err := ctx.Err(); err != nil {
		return pluginproto.Observation{}, err
	}
	if w.Manifest.PluginID != "musicfs.plugin.source.localfiles" || w.Access == nil || w.state == nil {
		return pluginproto.Observation{}, fmt.Errorf("UNSUPPORTED: provider cannot rebind local identities")
	}
	scope := pluginproto.ScanScope{RootID: r.RootID, RootIdentity: r.RootIdentity, Recursive: true, MaxDepth: 0}
	if !localJobID.MatchString(r.JobID) || !localExternalID.MatchString(r.ExternalID) || r.OldIdentity == "" || r.NewIdentity == "" || r.OldIdentity == r.NewIdentity || r.OldVersion == "" || r.NewVersion == "" || !scope.Contains(r.RootID, r.RelativePath) {
		return pluginproto.Observation{}, fmt.Errorf("INVALID_ARGUMENT: invalid local rebind")
	}
	allowed := false
	for _, root := range w.Access.Roots() {
		if root.ID == r.RootID && root.ExpectedIdentity == r.RootIdentity {
			allowed = true
		}
	}
	if !allowed {
		return pluginproto.Observation{}, fmt.Errorf("ACCESS_DENIED: writeback root is not authorized")
	}
	entry, err := w.Access.Inspect(r.RootID, r.RelativePath)
	if err != nil || entry.FileIdentity != r.NewIdentity || entry.ObservedVersion != r.NewVersion || entry.LinkCount != 1 {
		return pluginproto.Observation{}, fmt.Errorf("RESOURCE_CHANGED: replaced source no longer matches")
	}
	w.state.mu.Lock()
	defer w.state.mu.Unlock()
	intent, value, err := w.state.intent()
	if err != nil {
		return pluginproto.Observation{}, err
	}
	if intent != nil && intent.Request.JobID == r.JobID {
		if intent.Request != r {
			return pluginproto.Observation{}, fmt.Errorf("CONFLICT: writeback mapping parameters changed")
		}
		if !intent.Complete {
			if err = w.state.applyRebind(ctx, intent, value); err != nil {
				return pluginproto.Observation{}, err
			}
		}
		if err = w.state.checkCompleted(r); err != nil {
			return pluginproto.Observation{}, err
		}
		w.mu.Lock()
		clear(w.snapshots)
		w.mu.Unlock()
		return intent.Observation, nil
	}
	if intent != nil && !intent.Complete {
		return pluginproto.Observation{}, fmt.Errorf("CONFLICT: another local mapping needs recovery")
	}
	oldState, err := w.state.get(aliasKey(r.RootID, r.OldIdentity))
	if err != nil {
		return pluginproto.Observation{}, err
	}
	newState, err := w.state.get(aliasKey(r.RootID, r.NewIdentity))
	if err != nil {
		return pluginproto.Observation{}, err
	}
	trackState, err := w.state.get("track-" + r.ExternalID)
	if err != nil {
		return pluginproto.Observation{}, err
	}
	oldAliases, err := decodeAliases(oldState)
	if err != nil {
		return pluginproto.Observation{}, err
	}
	newAliases, err := decodeAliases(newState)
	if err != nil {
		return pluginproto.Observation{}, err
	}
	if oldAliases.Paths[r.RelativePath] != r.ExternalID || len(newAliases.Paths) != 0 {
		return pluginproto.Observation{}, fmt.Errorf("CONFLICT: local identity mapping is not empty or no longer owned")
	}
	var observation pluginproto.Observation
	if json.Unmarshal(trackState.Value, &observation) != nil || observation.ExternalID != r.ExternalID || observation.RootID != r.RootID || observation.RelativePath != r.RelativePath || observation.ObservedVersion != r.OldVersion {
		return pluginproto.Observation{}, fmt.Errorf("RESOURCE_CHANGED: local track observation changed")
	}
	delete(oldAliases.Paths, r.RelativePath)
	newAliases.Paths[r.RelativePath] = r.ExternalID
	observation.ObservedVersion, observation.SizeBytes, observation.MIME = r.NewVersion, entry.SizeBytes, entry.MIME
	observation.ProviderSHA512 = nil
	observation.BitrateBps, observation.BitrateMode = nil, nil
	intent = &rebindIntent{Request: r, Observation: observation, Changes: []rebindChange{
		makeChange(aliasKey(r.RootID, r.NewIdentity), newState, newAliases),
		makeChange("track-"+r.ExternalID, trackState, observation),
		makeChange(aliasKey(r.RootID, r.OldIdentity), oldState, oldAliases),
	}}
	raw, _ := json.Marshal(intent)
	value = pluginproto.StateValue{Revision: value.Revision + 1, Value: raw}
	if err = w.state.put(rebindKey, value); err != nil {
		return pluginproto.Observation{}, err
	}
	if err = w.state.applyRebind(ctx, intent, value); err != nil {
		return pluginproto.Observation{}, err
	}
	// Any cursor from before replacement describes old bytes. Provider-wide
	// coordination ensures no active scan is interrupted by this invalidation.
	w.mu.Lock()
	clear(w.snapshots)
	w.mu.Unlock()
	return observation, nil
}
func decodeAliases(state pluginproto.StateValue) (localAliases, error) {
	a := localAliases{Paths: map[string]string{}}
	if len(state.Value) > 0 && (json.Unmarshal(state.Value, &a) != nil || a.Paths == nil || len(a.Paths) > 1024) {
		return a, fmt.Errorf("RESOURCE_CHANGED: invalid local identity mapping")
	}
	return a, nil
}
func makeChange(key string, old pluginproto.StateValue, value any) rebindChange {
	raw, _ := json.Marshal(value)
	return rebindChange{Key: key, Before: old, After: pluginproto.StateValue{Revision: old.Revision + 1, Value: raw}}
}
