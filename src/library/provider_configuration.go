package library

import (
	"encoding/json"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
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
	return &declaration
}

func providerRoots(r Runtime) []ProviderRoot {
	result := make([]ProviderRoot, 0, len(r.RootNames))
	for id, name := range r.RootNames {
		result = append(result, ProviderRoot{ID: id, Name: name})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
