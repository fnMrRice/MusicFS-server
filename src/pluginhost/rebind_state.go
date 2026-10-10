package pluginhost

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

// Call with stateStore.mu held. The durable intent itself is the restart
// barrier: worker state requests are blocked until the host finishes recovery.
func (s *stateStore) intent() (*rebindIntent, pluginproto.StateValue, error) {
	v, err := s.get(rebindKey)
	if err != nil {
		return nil, v, err
	}
	if len(v.Value) == 0 {
		return nil, v, nil
	}
	var i rebindIntent
	if json.Unmarshal(v.Value, &i) != nil || len(i.Changes) != 3 || !localJobID.MatchString(i.Request.JobID) {
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

func (s *stateStore) checkCompleted(r LocalRebind) error {
	n, err := s.get(aliasKey(r.RootID, r.NewIdentity))
	if err != nil {
		return err
	}
	a, err := decodeAliases(n)
	if err != nil {
		return err
	}
	o, err := s.get(aliasKey(r.RootID, r.OldIdentity))
	if err != nil {
		return err
	}
	b, err := decodeAliases(o)
	if err != nil {
		return err
	}
	t, err := s.get("track-" + r.ExternalID)
	if err != nil {
		return err
	}
	var observation pluginproto.Observation
	if a.Paths[r.RelativePath] != r.ExternalID || b.Paths[r.RelativePath] != "" || json.Unmarshal(t.Value, &observation) != nil || observation.ExternalID != r.ExternalID || observation.RootID != r.RootID || observation.RelativePath != r.RelativePath || observation.ObservedVersion != r.NewVersion {
		return fmt.Errorf("CONFLICT: completed local mapping changed")
	}
	return nil
}
func sameState(a, b pluginproto.StateValue) bool {
	return a.Revision == b.Revision && bytes.Equal(a.Value, b.Value)
}
func (s *stateStore) applyRebind(ctx context.Context, i *rebindIntent, journal pluginproto.StateValue) error {
	// Check every expected mapping before changing any of them. A previous
	// partial run may have installed an exact After value; no other change wins.
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
