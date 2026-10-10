// Package pluginconfig owns official plugin configuration declarations shared by
// the plugin handshake and host configuration loader.
package pluginconfig

import "github.com/fnMrRice/MusicFS-server/src/pluginproto"

func LocalFiles() *pluginproto.Configuration {
	return &pluginproto.Configuration{
		SchemaRevision:     "local-import-v3",
		ImportDefaults:     pluginproto.ImportOptions{Recursive: true, MaxDepth: 64, ExcludePatterns: []string{}},
		UnavailableOptions: map[string]pluginproto.UnavailableOption{},
		ImportOptionsSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"recursive", "maxDepth", "importLyrics", "excludePatterns"},
			"properties": map[string]any{
				"recursive":       map[string]any{"type": "boolean", "default": true, "title": "递归扫描", "description": "包括子目录。关闭时只扫描根目录中的文件，深度为 0；重新开启且未指定深度时恢复为 64。"},
				"maxDepth":        map[string]any{"type": "integer", "minimum": 0, "maximum": 64, "default": 64, "title": "最大递归深度", "description": "根目录中的文件为第 0 层。开启递归时允许 1 至 64，关闭时只能为 0。"},
				"importLyrics":    map[string]any{"type": "boolean", "default": false, "title": "同步歌词", "description": "扫描成功后导入内嵌或同名旁置歌词，只补充缺失且未锁定的内容；不修改音乐原件。"},
				"excludePatterns": map[string]any{"type": "array", "maxItems": 32, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 256}, "default": []string{}, "title": "排除路径", "description": "根相对路径，使用 / 分隔，大小写敏感。* 与 ? 匹配单层，独立 ** 匹配任意层；匹配目录排除整支。最多 32 条，每条 UTF-8 最多 256 字节。不支持绝对路径、..、反斜线、字符类或否定。排除范围的既有曲目保留，不标记缺失。"},
			},
			"if":   map[string]any{"properties": map[string]any{"recursive": map[string]any{"const": true}}, "required": []string{"recursive"}},
			"then": map[string]any{"properties": map[string]any{"maxDepth": map[string]any{"minimum": 1}}},
			"else": map[string]any{"properties": map[string]any{"maxDepth": map[string]any{"const": 0}}},
		},
	}
}
