package directoryidentity

import (
	"bytes"
	"fmt"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// ValidatePlan 从变更前的状态重建计划，拒绝损坏日志对无关键或版本的写入。
func ValidatePlan(r Rebind, observation pluginproto.Observation, changes []Change) error {
	invalid := fmt.Errorf("RESOURCE_CHANGED: invalid directory rebind plan")
	scope := pluginproto.ScanScope{RootID: r.RootID, RootIdentity: r.RootIdentity, Recursive: true}
	if !ValidID(r.ExternalID) || r.OldIdentity == "" || r.NewIdentity == "" || r.OldIdentity == r.NewIdentity || r.OldVersion == "" || r.NewVersion == "" || !scope.Contains(r.RootID, r.RelativePath) || len(changes) != 3 || observation.SizeBytes < 0 {
		return invalid
	}
	keys := []string{AliasKey(r.RootID, r.NewIdentity), TrackKey(r.ExternalID), AliasKey(r.RootID, r.OldIdentity)}
	before := map[string]pluginproto.StateValue{}
	for i, change := range changes {
		if change.Key != keys[i] || change.Before.Revision < 0 || change.After.Revision <= 0 || change.After.Revision != change.Before.Revision+1 || len(change.Before.Value) > 512<<10 || len(change.After.Value) > 512<<10 {
			return invalid
		}
		before[change.Key] = change.Before
	}
	expected, computed, err := Prepare(func(key string) (pluginproto.StateValue, error) { return before[key], nil }, r, pluginproto.Entry{SizeBytes: observation.SizeBytes, MIME: observation.MIME})
	if err != nil || !reflect.DeepEqual(observation, expected) {
		return invalid
	}
	for i, change := range changes {
		if !bytes.Equal(change.After.Value, computed[i].After.Value) {
			return invalid
		}
	}
	return nil
}
