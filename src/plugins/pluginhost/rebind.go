package pluginhost

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// LocalRebind 是可信宿主操作；调用者须持续持有 Provider 实例的排他写回租约。
type LocalRebind = directoryidentity.Rebind
type rebindIntent struct {
	Request     LocalRebind                `json:"request"`
	Observation pluginproto.Observation    `json:"observation"`
	Changes     []directoryidentity.Change `json:"changes"`
	Complete    bool                       `json:"complete"`
}

const rebindKey = "host-rebind-intent"

var localJobID = regexp.MustCompile(`^[0-9a-f]{48}$`)

func (w *Worker) RebindLocalTrack(ctx context.Context, r LocalRebind) (pluginproto.Observation, error) {
	if err := ctx.Err(); err != nil {
		return pluginproto.Observation{}, err
	}
	if !w.Supports(directoryidentity.Capability) || w.Access == nil || w.state == nil {
		return pluginproto.Observation{}, fmt.Errorf("UNSUPPORTED: provider cannot rebind local identities")
	}
	scope := pluginproto.ScanScope{RootID: r.RootID, RootIdentity: r.RootIdentity, Recursive: true, MaxDepth: 0}
	if !localJobID.MatchString(r.JobID) || !directoryidentity.ValidID(r.ExternalID) || r.OldIdentity == "" || r.NewIdentity == "" || r.OldIdentity == r.NewIdentity || r.OldVersion == "" || r.NewVersion == "" || !scope.Contains(r.RootID, r.RelativePath) {
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
		if err = directoryidentity.CheckCompleted(w.state.get, r); err != nil {
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
	observation, changes, err := directoryidentity.Prepare(w.state.get, r, entry)
	if err != nil {
		return pluginproto.Observation{}, err
	}
	if err := directoryidentity.ValidatePlan(r, observation, changes); err != nil {
		return pluginproto.Observation{}, err
	}
	intent = &rebindIntent{Request: r, Observation: observation, Changes: changes}
	raw, _ := json.Marshal(intent)
	value = pluginproto.StateValue{Revision: value.Revision + 1, Value: raw}
	if err = w.state.put(rebindKey, value); err != nil {
		return pluginproto.Observation{}, err
	}
	if err = w.state.applyRebind(ctx, intent, value); err != nil {
		return pluginproto.Observation{}, err
	}
	// 替换前的游标描述旧内容；实例排他协调保证失效操作不打断活动扫描。
	w.mu.Lock()
	clear(w.snapshots)
	w.mu.Unlock()
	return observation, nil
}
