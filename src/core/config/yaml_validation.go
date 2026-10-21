package config

import (
	"fmt"
	"math"
	"strconv"

	"go.yaml.in/yaml/v3"
)

func nodeError(node *yaml.Node, field, message string) error {
	return fmt.Errorf("configuration %s at line %d column %d: %s", field, node.Line, node.Column, message)
}

func validateNodes(node *yaml.Node, depth int) error {
	if depth > 32 {
		return nodeError(node, "document", "nesting exceeds 32 levels")
	}
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return nodeError(node, "document", "anchors and aliases are not supported")
	}
	switch node.Tag {
	case "!!str", "!!int", "!!float", "!!bool", "!!map", "!!seq":
	default:
		return nodeError(node, "document", "null, merge keys and unsupported tags or scalar types are not allowed")
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "<<" {
				return nodeError(key, "document", "mapping keys must be ordinary strings")
			}
			if seen[key.Value] {
				return nodeError(key, "document", "duplicate mapping key")
			}
			seen[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateNodes(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

type fieldShape struct {
	tag      string
	fields   map[string]fieldShape
	items    *fieldShape
	values   *fieldShape
	required []string
}

var stringShape = fieldShape{tag: "!!str"}
var intShape = fieldShape{tag: "!!int"}
var boolShape = fieldShape{tag: "!!bool"}

func sequenceShape(item fieldShape) fieldShape { return fieldShape{tag: "!!seq", items: &item} }
func mappingShape(fields map[string]fieldShape, required ...string) fieldShape {
	return fieldShape{tag: "!!map", fields: fields, required: required}
}

func validateShape(node *yaml.Node, shape fieldShape, path string) error {
	if shape.tag == "number" {
		if node.Tag != "!!int" && node.Tag != "!!float" {
			return nodeError(node, path, "expected a finite number")
		}
		value, err := strconv.ParseFloat(node.Value, 64)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
			return nodeError(node, path, "expected a finite number")
		}
		return nil
	}
	if node.Tag != shape.tag {
		return nodeError(node, path, "invalid field type")
	}
	if shape.tag == "!!bool" && node.Value != "true" && node.Value != "false" {
		return nodeError(node, path, "boolean must be true or false")
	}
	if shape.tag == "!!int" {
		value, err := strconv.ParseInt(node.Value, 10, 64)
		if err != nil || strconv.FormatInt(value, 10) != node.Value {
			return nodeError(node, path, "integer must use canonical decimal notation")
		}
	}
	if shape.tag == "!!seq" {
		for _, item := range node.Content {
			if err := validateShape(item, *shape.items, path+"[]"); err != nil {
				return err
			}
		}
	}
	if shape.tag == "!!map" && shape.fields != nil {
		seen := map[string]bool{}
		for i := 0; i < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			field, ok := shape.fields[key.Value]
			if !ok {
				return nodeError(key, path, "unknown field")
			}
			seen[key.Value] = true
			if err := validateShape(value, field, path+"."+key.Value); err != nil {
				return err
			}
		}
		for _, required := range shape.required {
			if !seen[required] {
				return nodeError(node, path+"."+required, "required field is missing")
			}
		}
	}
	if shape.tag == "!!map" && shape.values != nil {
		for i := 0; i < len(node.Content); i += 2 {
			if err := validateShape(node.Content[i+1], *shape.values, path+"."+node.Content[i].Value); err != nil {
				return err
			}
		}
	}
	return nil
}

func yamlField(node *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func validateDocumentShape(root *yaml.Node) error {
	instance := mappingShape(map[string]fieldShape{
		"instanceId": stringShape, "name": stringShape, "ownerUserId": stringShape,
		"enabled": boolShape, "config": {tag: "!!map"}, "importDefaults": importDefaultsShape(),
		"permissions": mappingShape(map[string]fieldShape{"readDirectories": sequenceShape(stringShape)}, "readDirectories"),
	}, "instanceId", "config")
	plugin := mappingShape(map[string]fieldShape{
		"pluginId": stringShape, "executable": stringShape, "sha256": stringShape, "execution": stringShape,
		"args":    sequenceShape(stringShape),
		"enabled": boolShape, "instances": sequenceShape(instance),
	}, "pluginId", "instances")
	shape := mappingShape(map[string]fieldShape{
		"storage": storageShape(), "audioDSP": audioDSPShape(), "audioChannels": audioChannelsShape(), "logging": loggingShape(),
		"pluginBundle":  stringShape,
		"configVersion": intShape, "listen": stringShape, "dataRoot": stringShape,
		"mediaRoot": stringShape, "cacheRoot": stringShape, "webRoot": stringShape,
		"allowLoopbackHttp": boolShape, "allowedOrigins": sequenceShape(stringShape),
		"tlsCert": stringShape, "tlsKey": stringShape, "plugins": sequenceShape(plugin),
		"maxSessions": intShape, "maxProcessing": intShape, "maxCacheBytes": intShape,
		"sessionTtlSeconds": intShape,
		"hashing": mappingShape(map[string]fieldShape{
			"auto": boolShape, "maxConcurrent": intShape, "bytesPerSecond": intShape,
		}),
		"fingerprinting": mappingShape(map[string]fieldShape{
			"enabled": boolShape, "fpcalcPath": stringShape, "fpcalcSha256": stringShape,
			"maxConcurrent": intShape, "timeoutSeconds": intShape,
		}),
		"metadata": mappingShape(map[string]fieldShape{
			"automatic": mappingShape(map[string]fieldShape{
				"enabled": boolShape, "instancePriority": sequenceShape(stringShape),
				"fields": sequenceShape(stringShape), "languages": sequenceShape(stringShape),
			}),
		}),
		"writeBack": mappingShape(map[string]fieldShape{
			"enabled": boolShape, "allowOverwrite": boolShape, "maxFileBytes": intShape,
			"roots": sequenceShape(mappingShape(map[string]fieldShape{
				"instanceId": stringShape, "rootId": stringShape, "fields": sequenceShape(stringShape),
			}, "instanceId", "rootId", "fields")),
		}),
	}, "configVersion")
	if err := validateShape(root, shape, "config"); err != nil {
		return err
	}
	if err := validateLoggingNodes(root); err != nil {
		return err
	}
	if channels := yamlField(root, "audioChannels"); channels != nil {
		if layout := yamlField(channels, "outputLayout"); layout != nil && layout.Value == "" {
			return nodeError(layout, "config.audioChannels.outputLayout", "expected a supported channel layout")
		}
	}
	if version := yamlField(root, "configVersion"); version.Value != "1" {
		return nodeError(version, "config.configVersion", "unsupported configuration version")
	}
	plugins := yamlField(root, "plugins")
	if plugins == nil {
		return nil
	}
	musicRoot := mappingShape(map[string]fieldShape{"id": stringShape, "name": stringShape, "path": stringShape, "importDefaults": importDefaultsShape()}, "id", "name", "path")
	for _, plugin := range plugins.Content {
		id := yamlField(plugin, "pluginId")
		var settings fieldShape
		switch id.Value {
		case "musicfs.plugin.source.localfiles":
			settings = mappingShape(map[string]fieldShape{"musicRoots": sequenceShape(musicRoot), "watch": watchShape()})
		case "musicfs.plugin.decoder.ffmpeg":
			settings = mappingShape(map[string]fieldShape{"ffmpegPath": stringShape, "ffprobePath": stringShape})
		case "musicfs.plugin.decoder.ncm", "musicfs.scraper.local", "musicfs.plugin.source.netease", "musicfs.plugin.source.qqmusic", "musicfs.plugin.source.applemusic":
			settings = mappingShape(map[string]fieldShape{})
		default:
			return nodeError(id, "config.plugins[].pluginId", "unsupported plugin")
		}
		for _, instance := range yamlField(plugin, "instances").Content {
			if owner := yamlField(instance, "ownerUserId"); owner != nil && owner.Value == "" {
				return nodeError(owner, "config.plugins[].instances[].ownerUserId", "expected a nonempty user identity")
			}
			if err := validateShape(yamlField(instance, "config"), settings, "config.plugins[].instances[].config"); err != nil {
				return err
			}
		}
	}
	return nil
}
