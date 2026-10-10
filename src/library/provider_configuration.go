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
	declaration.ImportDefaults.ExcludePatterns = append([]string{}, r.ImportDefaults.ExcludePatterns...)
	return &declaration
}

func providerRoots(r Runtime) []ProviderRoot {
	result := make([]ProviderRoot, 0, len(r.RootNames))
	for id, name := range r.RootNames {
		root := ProviderRoot{ID: id, Name: name}
		if c, ok := r.WriteBack[id]; ok {
			c.Fields = append([]string{}, c.Fields...)
			c.Formats = append([]string{}, c.Formats...)
			root.WriteBack = &c
		}
		result = append(result, root)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
