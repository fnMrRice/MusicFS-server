package pluginhost

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

type stateStore struct {
	dir string
	mu  sync.Mutex
}

var stateKey = regexp.MustCompile(`^[a-z0-9_-]{1,80}$`)

func (s *stateStore) get(key string) (pluginproto.StateValue, error) {
	if !stateKey.MatchString(key) {
		return pluginproto.StateValue{}, fmt.Errorf("ACCESS_DENIED: invalid state key")
	}
	b, err := os.ReadFile(filepath.Join(s.dir, key+".json"))
	if os.IsNotExist(err) {
		return pluginproto.StateValue{}, nil
	}
	if err != nil {
		return pluginproto.StateValue{}, err
	}
	if len(b) > 8<<20 {
		return pluginproto.StateValue{}, fmt.Errorf("RATE_LIMITED: state limit exceeded")
	}
	var v pluginproto.StateValue
	err = json.Unmarshal(b, &v)
	return v, err
}
func (s *stateStore) load(key string) (pluginproto.StateValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(key)
}
func (s *stateStore) save(r pluginproto.StateRequest) (pluginproto.StateValue, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(r.Value) > 512<<10 {
		return pluginproto.StateValue{}, fmt.Errorf("RATE_LIMITED: state value too large")
	}
	old, err := s.get(r.Key)
	if err != nil {
		return old, err
	}
	if old.Revision != r.Revision {
		return old, fmt.Errorf("RESOURCE_CHANGED: state revision mismatch")
	}
	v := pluginproto.StateValue{Revision: old.Revision + 1, Value: r.Value}
	b, err := json.Marshal(v)
	if err != nil {
		return v, err
	}
	if err = os.MkdirAll(s.dir, 0700); err != nil {
		return v, err
	}
	tmp, err := os.CreateTemp(s.dir, "state-*")
	if err != nil {
		return v, err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err = tmp.Write(b); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return v, err
	}
	err = os.Rename(name, filepath.Join(s.dir, r.Key+".json"))
	return v, err
}
