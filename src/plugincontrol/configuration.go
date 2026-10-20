package plugincontrol

import (
	"encoding/json"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type Root struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	PathConfigured bool   `json:"pathConfigured"`
}
type Configuration struct {
	PluginID          string                     `json:"pluginId"`
	InstanceID        string                     `json:"instanceId"`
	Revision          string                     `json:"revision"`
	Name              string                     `json:"name"`
	Roots             []Root                     `json:"roots"`
	Watch             *config.WatchConfig        `json:"watch"`
	ImportOptions     *pluginproto.Configuration `json:"importOptions"`
	FFmpegConfigured  bool                       `json:"ffmpegConfigured"`
	FFprobeConfigured bool                       `json:"ffprobeConfigured"`
	Schema            map[string]any             `json:"schema"`
}

func (m *Manager) Configuration(id string) (Configuration, error) {
	m.mu.Lock()
	r := m.runtimes[id]
	if r == nil {
		m.mu.Unlock()
		return Configuration{}, domain.Fail("NOT_FOUND", "Plugin instance not found")
	}
	p, i := r.plugin, r.instance
	revision := m.revision
	m.mu.Unlock()
	v := Configuration{PluginID: p.PluginID, InstanceID: i.InstanceID, Revision: revision, Name: i.Name, Roots: []Root{}, FFmpegConfigured: i.Config.FFmpegPath != "", FFprobeConfigured: i.Config.FFprobePath != "", Schema: settingsSchema(p.PluginID)}
	for _, r := range i.Config.MusicRoots {
		v.Roots = append(v.Roots, Root{ID: r.ID, Name: r.Name, PathConfigured: r.Path != ""})
	}
	if p.PluginID == "musicfs.plugin.source.localfiles" {
		watch := config.DefaultWatch()
		if i.Config.Watch != nil {
			watch = *i.Config.Watch
		}
		v.Watch = &watch
		declaration := pluginconfig.LocalFiles()
		defaults, e := config.EffectiveImportDefaults(p.PluginID, i)
		if e != nil {
			return Configuration{}, e
		}
		declaration.ImportDefaults = defaults
		v.ImportOptions = declaration
	}
	return v, nil
}

// ValidateSettings uses the same declaration exposed to the administrator UI.
// Detailed schema errors are intentionally not returned because paths are write-only.
func ValidateSettings(plugin string, raw json.RawMessage) (config.InstanceSettings, error) {
	var value any
	var input config.InstanceSettings
	if json.Unmarshal(raw, &value) != nil {
		return input, domain.Fail("INVALID_ARGUMENT", "Invalid plugin settings")
	}
	b, _ := json.Marshal(settingsSchema(plugin))
	var declaration any
	_ = json.Unmarshal(b, &declaration)
	compiler := jsonschema.NewCompiler()
	const resource = "https://musicfs.invalid/internal/instance-settings"
	if err := compiler.AddResource(resource, declaration); err != nil {
		return input, err
	}
	schema, err := compiler.Compile(resource)
	if err != nil {
		return input, err
	}
	if schema.Validate(value) != nil || json.Unmarshal(raw, &input) != nil {
		return input, domain.Fail("INVALID_ARGUMENT", "Plugin settings do not match the declared schema")
	}
	return input, nil
}

func settingsSchema(plugin string) map[string]any {
	text := func(max int) map[string]any { return map[string]any{"type": "string", "maxLength": max} }
	path := func() map[string]any {
		return map[string]any{"type": "string", "minLength": 1, "maxLength": 4096, "writeOnly": true}
	}
	properties := map[string]any{"name": text(256)}
	if plugin == "musicfs.plugin.source.localfiles" {
		properties["importDefaults"] = pluginconfig.LocalFiles().ImportOptionsSchema
		properties["musicRoots"] = map[string]any{"type": "array", "minItems": 0, "maxItems": 256, "items": map[string]any{"type": "object", "required": []string{"id", "name"}, "additionalProperties": false, "properties": map[string]any{"id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}, "name": text(256), "path": path()}}}
		properties["watch"] = map[string]any{"type": "object", "required": []string{"enabled", "debounceMs", "maxDelayMs", "fullRescanSeconds"}, "additionalProperties": false, "properties": map[string]any{"enabled": map[string]any{"type": "boolean"}, "debounceMs": map[string]any{"type": "integer", "minimum": 100, "maximum": 30000}, "maxDelayMs": map[string]any{"type": "integer", "minimum": 100, "maximum": 60000}, "fullRescanSeconds": map[string]any{"type": "integer", "minimum": 30, "maximum": 86400}}}
	}
	if plugin == "musicfs.plugin.decoder.ffmpeg" {
		properties["ffmpegPath"] = path()
		properties["ffprobePath"] = path()
	}
	return map[string]any{"$schema": "https://json-schema.org/draft/2020-12/schema", "type": "object", "additionalProperties": false, "minProperties": 1, "properties": properties}
}
