package directoryidentity

import (
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// Rebind 由宿主从已发布的替换任务构造，不能直接接收插件或公开请求。
type Rebind struct {
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

type Change struct {
	Key           string `json:"key"`
	Before, After pluginproto.StateValue
}

type ReadState func(string) (pluginproto.StateValue, error)

// Prepare 不写状态；调用者持有实例排他租约并先验证根、路径及新文件版本。
// 返回的三项变更必须作为带持久化意图的同一事务发布并支持中断恢复。
func Prepare(read ReadState, r Rebind, entry pluginproto.Entry) (pluginproto.Observation, []Change, error) {
	oldState, err := read(AliasKey(r.RootID, r.OldIdentity))
	if err != nil {
		return pluginproto.Observation{}, nil, err
	}
	newState, err := read(AliasKey(r.RootID, r.NewIdentity))
	if err != nil {
		return pluginproto.Observation{}, nil, err
	}
	trackState, err := read(TrackKey(r.ExternalID))
	if err != nil {
		return pluginproto.Observation{}, nil, err
	}
	oldAliases, err := DecodeAliases(oldState)
	if err != nil {
		return pluginproto.Observation{}, nil, err
	}
	newAliases, err := DecodeAliases(newState)
	if err != nil {
		return pluginproto.Observation{}, nil, err
	}
	if oldAliases.Paths[r.RelativePath] != r.ExternalID || len(newAliases.Paths) != 0 {
		return pluginproto.Observation{}, nil, fmt.Errorf("CONFLICT: directory identity mapping is not empty or no longer owned")
	}
	var observation pluginproto.Observation
	if json.Unmarshal(trackState.Value, &observation) != nil || observation.ExternalID != r.ExternalID || observation.RootID != r.RootID || observation.RelativePath != r.RelativePath || observation.ObservedVersion != r.OldVersion {
		return pluginproto.Observation{}, nil, fmt.Errorf("RESOURCE_CHANGED: directory track observation changed")
	}
	delete(oldAliases.Paths, r.RelativePath)
	newAliases.Paths[r.RelativePath] = r.ExternalID
	observation.ObservedVersion, observation.SizeBytes, observation.MIME = r.NewVersion, entry.SizeBytes, entry.MIME
	observation.ProviderSHA512 = nil
	observation.BitrateBps, observation.BitrateMode = nil, nil
	return observation, []Change{
		makeChange(AliasKey(r.RootID, r.NewIdentity), newState, newAliases),
		makeChange(TrackKey(r.ExternalID), trackState, observation),
		makeChange(AliasKey(r.RootID, r.OldIdentity), oldState, oldAliases),
	}, nil
}

func CheckCompleted(read ReadState, r Rebind) error {
	n, err := read(AliasKey(r.RootID, r.NewIdentity))
	if err != nil {
		return err
	}
	a, err := DecodeAliases(n)
	if err != nil {
		return err
	}
	o, err := read(AliasKey(r.RootID, r.OldIdentity))
	if err != nil {
		return err
	}
	b, err := DecodeAliases(o)
	if err != nil {
		return err
	}
	t, err := read(TrackKey(r.ExternalID))
	if err != nil {
		return err
	}
	var observation pluginproto.Observation
	if a.Paths[r.RelativePath] != r.ExternalID || b.Paths[r.RelativePath] != "" || json.Unmarshal(t.Value, &observation) != nil || observation.ExternalID != r.ExternalID || observation.RootID != r.RootID || observation.RelativePath != r.RelativePath || observation.ObservedVersion != r.NewVersion {
		return fmt.Errorf("CONFLICT: completed directory mapping changed")
	}
	return nil
}

func makeChange(key string, old pluginproto.StateValue, value any) Change {
	raw, _ := json.Marshal(value)
	return Change{Key: key, Before: old, After: pluginproto.StateValue{Revision: old.Revision + 1, Value: raw}}
}
