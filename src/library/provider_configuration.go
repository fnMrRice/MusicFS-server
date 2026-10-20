package library

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func safeProviderConfiguration(r Runtime) *pluginproto.Configuration {
	if r.Configuration == nil {
		return nil
	}
	// Return an independent descriptor; callers cannot mutate the loaded defaults.
	raw, err := json.Marshal(r.Configuration)
	if err != nil {
		return nil
	}
	var declaration pluginproto.Configuration
	if json.Unmarshal(raw, &declaration) != nil {
		return nil
	}
	declaration.ImportDefaults = r.ImportDefaults
	declaration.ImportDefaults.ExcludePatterns = append([]string{}, r.ImportDefaults.ExcludePatterns...)
	return &declaration
}

func (s *Service) providerRoots(ctx context.Context, r Runtime) []ProviderRoot {
	s.mu.RLock()
	ready, hasProbe := s.writeBackProbeReady, s.probe != nil
	s.mu.RUnlock()
	if ready != nil {
		hasProbe = ready()
	}
	workerReady := r.Worker != nil && r.Gate.Check(ctx) == nil
	if workerReady {
		select {
		case <-r.Worker.Done():
			workerReady = false
		default:
		}
	}
	result := make([]ProviderRoot, 0, len(r.RootNames))
	for id, name := range r.RootNames {
		root := ProviderRoot{ID: id, Name: name}
		if c, ok := r.WriteBack[id]; ok {
			c.Available = c.Available && hasProbe && workerReady
			c.Fields = append([]string{}, c.Fields...)
			c.Formats = append([]string{}, c.Formats...)
			root.WriteBack = &c
		}
		result = append(result, root)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
