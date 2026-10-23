package pluginhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/sdk/directoryidentity"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// 调用时持有 stateStore.mu；持久化意图阻止重启后的状态访问，直到恢复完成。
func (s *stateStore) intent() (*rebindIntent, pluginproto.StateValue, error) {
	v, err := s.get(rebindKey)
	if err != nil {
		return nil, v, err
	}
	if len(v.Value) == 0 {
		return nil, v, nil
	}
	var i rebindIntent
	if json.Unmarshal(v.Value, &i) != nil || !localJobID.MatchString(i.Request.JobID) || directoryidentity.ValidatePlan(i.Request, i.Observation, i.Changes) != nil {
		return nil, v, fmt.Errorf("RESOURCE_CHANGED: invalid local mapping journal")
	}
	return &i, v, nil
}
func (s *stateStore) ready() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyLocked()
}
func (s *stateStore) readyLocked() error {
	if !s.declarationReady {
		return fmt.Errorf("ACCESS_DENIED: provider handshake is not complete")
	}
	if !s.rebindEnabled {
		return nil
	}
	if s.rebindReady != nil {
		if *s.rebindReady {
			return nil
		}
		return fmt.Errorf("RESOURCE_CHANGED: local mapping needs writeback recovery")
	}
	i, _, err := s.intent()
	if err != nil {
		return err
	}
	if i != nil && !i.Complete {
		ready := false
		s.rebindReady = &ready
		return fmt.Errorf("RESOURCE_CHANGED: local mapping needs writeback recovery")
	}
	ready := true
	s.rebindReady = &ready
	return nil
}

func sameState(a, b pluginproto.StateValue) bool {
	return a.Revision == b.Revision && bytes.Equal(a.Value, b.Value)
}
func (s *stateStore) applyRebind(ctx context.Context, i *rebindIntent, journal pluginproto.StateValue) error {
	// 写入前核对全部预期状态；中断恢复仅允许精确的 Before 或 After 值。
	for _, change := range i.Changes {
		current, err := s.get(change.Key)
		if err != nil {
			return err
		}
		if !sameState(current, change.Before) && !sameState(current, change.After) {
			return fmt.Errorf("CONFLICT: local mapping changed during recovery")
		}
	}
	for _, change := range i.Changes {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := s.get(change.Key)
		if err != nil {
			return err
		}
		if !sameState(current, change.After) {
			if err = s.put(change.Key, change.After); err != nil {
				return err
			}
		}
	}
	i.Complete = true
	raw, _ := json.Marshal(i)
	return s.put(rebindKey, pluginproto.StateValue{Revision: journal.Revision + 1, Value: raw})
}
