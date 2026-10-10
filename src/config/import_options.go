package config

import (
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func EffectiveImportDefaults(pluginID string, instance Instance) (pluginproto.ImportOptions, error) {
	if pluginID != "musicfs.localfiles" {
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
