// Package pluginconfig owns official plugin configuration declarations shared by
// the plugin handshake and host configuration loader.
package pluginconfig

import "github.com/fnMrRice/MusicFS-server/src/pluginproto"

func LocalFiles() *pluginproto.Configuration {
	return &pluginproto.Configuration{
		SchemaRevision: "local-import-v1",
		ImportDefaults: pluginproto.ImportOptions{Recursive: true, MaxDepth: 64},
		UnavailableOptions: map[string]pluginproto.UnavailableOption{
			"importLyrics": {Code: "UNSUPPORTED", Reason: "歌词导入尚未实现"},
		},
		ImportOptionsSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"recursive", "maxDepth", "importLyrics"},
			"properties": map[string]any{
				"recursive":    map[string]any{"type": "boolean", "default": true, "title": "递归扫描", "description": "包括子目录。关闭时只扫描根目录中的文件，深度为 0；重新开启且未指定深度时恢复为 64。"},
				"maxDepth":     map[string]any{"type": "integer", "minimum": 0, "maximum": 64, "default": 64, "title": "最大递归深度", "description": "根目录中的文件为第 0 层。开启递归时允许 1 至 64，关闭时只能为 0。"},
				"importLyrics": map[string]any{"type": "boolean", "default": false, "title": "同步歌词", "description": "尚未实现；请求开启会返回 UNSUPPORTED。"},
			},
			"if":   map[string]any{"properties": map[string]any{"recursive": map[string]any{"const": true}}, "required": []string{"recursive"}},
			"then": map[string]any{"properties": map[string]any{"maxDepth": map[string]any{"minimum": 1}}},
			"else": map[string]any{"properties": map[string]any{"maxDepth": map[string]any{"const": 0}}},
		},
	}
}
