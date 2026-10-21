package config

import (
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func EffectiveImportDefaults(pluginID string, instance Instance) (pluginproto.ImportOptions, error) {
	if pluginID != "musicfs.plugin.source.localfiles" {
		if instance.ImportDefaults != nil {
			return pluginproto.ImportOptions{}, errors.New("importDefaults requires a provider configuration declaration")
		}
		return pluginproto.ImportOptions{}, nil
	}
	declaration := pluginconfig.LocalFiles()
	patch := pluginproto.ImportOverrides{}
	if instance.ImportDefaults != nil {
		patch = *instance.ImportDefaults
	}
	options, err := pluginproto.MergeImportOptions(declaration.ImportDefaults, patch)
	if err != nil {
		return options, err
	}
	return options, declaration.ValidateOptions(options)
}

// Root defaults overlay instance defaults; omitted overrides retain inheritance.
func EffectiveRootImportDefaults(instance Instance, root MusicRoot) (pluginproto.ImportOptions, error) {
	base, err := EffectiveImportDefaults("musicfs.plugin.source.localfiles", instance)
	if err != nil {
		return base, err
	}
	if root.ImportDefaults != nil {
		base, err = pluginproto.MergeImportOptions(base, *root.ImportDefaults)
		if err != nil {
			return base, err
		}
	}
	return base, pluginconfig.LocalFiles().ValidateOptions(base)
}

func importDefaultsShape() fieldShape {
	properties := pluginconfig.LocalFiles().ImportOptionsSchema["properties"].(map[string]any)
	fields := make(map[string]fieldShape, len(properties))
	for name, raw := range properties {
		switch raw.(map[string]any)["type"] {
		case "boolean":
			fields[name] = boolShape
		case "integer":
			fields[name] = intShape
		case "string":
			fields[name] = stringShape
		case "array":
			fields[name] = sequenceShape(stringShape)
		}
	}
	return mappingShape(fields)
}
