package config

import (
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

func EffectiveImportDefaults(pluginID string, instance Instance) (pluginproto.ImportOptions, error) {
	declaration := pluginconfig.Configuration(pluginID)
	if declaration == nil {
		if instance.ImportDefaults != nil {
			return pluginproto.ImportOptions{}, errors.New("importDefaults requires a provider configuration declaration")
		}
		return pluginproto.ImportOptions{}, nil
	}
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
func EffectiveRootImportDefaults(pluginID string, instance Instance, root MusicRoot) (pluginproto.ImportOptions, error) {
	declaration := pluginconfig.Configuration(pluginID)
	if declaration == nil {
		return pluginproto.ImportOptions{}, errors.New("provider has no import configuration")
	}
	base, err := EffectiveImportDefaults(pluginID, instance)
	if err != nil {
		return base, err
	}
	if root.ImportDefaults != nil {
		base, err = pluginproto.MergeImportOptions(base, *root.ImportDefaults)
		if err != nil {
			return base, err
		}
	}
	return base, declaration.ValidateOptions(base)
}

func importDefaultsShape() fieldShape {
	// YAML 这里只验证公共 ImportOverrides 的形状，取值范围由插件声明验证。
	return mappingShape(map[string]fieldShape{"recursive": boolShape, "maxDepth": intShape,
		"importLyrics": boolShape, "importArtwork": boolShape, "excludePatterns": sequenceShape(stringShape),
		"skipHidden": boolShape, "respectNoMedia": boolShape})
}
