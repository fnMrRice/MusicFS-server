// Package pluginconfig owns official plugin configuration declarations shared by
// the plugin handshake and host configuration loader.
package pluginconfig

import "github.com/fnMrRice/MusicFS-server/sdk/pluginproto"

func LocalFiles() *pluginproto.Configuration {
	return &pluginproto.Configuration{
		SchemaRevision:     "local-import-v6",
		ImportDefaults:     pluginproto.ImportOptions{Recursive: true, MaxDepth: 0, ImportArtwork: true, ExcludePatterns: []string{}, SkipHidden: true, RespectNoMedia: true},
		UnavailableOptions: map[string]pluginproto.UnavailableOption{},
		ImportOptionsSchema: map[string]any{
			"type": "object", "additionalProperties": false,
			"required": []string{"recursive", "maxDepth", "importLyrics", "importArtwork", "excludePatterns", "skipHidden", "respectNoMedia"},
			"properties": map[string]any{
				"recursive":       map[string]any{"type": "boolean", "default": true, "title": "递归扫描", "description": "包括子目录。关闭时只扫描根目录中的文件，深度为 0；重新开启且未指定深度时恢复为不限深度。"},
				"maxDepth":        map[string]any{"type": "integer", "minimum": 0, "maximum": 64, "default": 0, "title": "最大递归深度", "description": "根目录中的文件为第 0 层。开启递归时 0 表示不限深度，1 至 64 表示最多进入的子目录层数；关闭时只能为 0。"},
				"skipHidden":      map[string]any{"type": "boolean", "default": true, "title": "跳过隐藏文件", "description": "跳过点开头的文件和目录，以及 Windows 隐藏属性条目。显式选择的根目录本身不因隐藏属性而跳过。"},
				"respectNoMedia":  map[string]any{"type": "boolean", "default": true, "title": "遵循 .nomedia", "description": "目录含 .nomedia 标记时跳过该目录及其子目录，包括所选根本身；只检查标记存在，不读取内容。被排除的旧曲库记录保留。"},
				"importLyrics":    map[string]any{"type": "boolean", "default": false, "title": "同步歌词", "description": "扫描成功后导入内嵌或同名旁置歌词，只补充缺失且未锁定的内容；不修改音乐原件。"},
				"importArtwork":   map[string]any{"type": "boolean", "default": true, "title": "导入封面", "description": "扫描成功后将内嵌或同名旁置封面保存到服务端数据目录，只补充缺失且未锁定的封面；不采用目录通用封面，不修改音乐原件。"},
				"excludePatterns": map[string]any{"type": "array", "maxItems": 32, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 256}, "default": []string{}, "title": "排除路径", "description": "根相对路径，使用 / 分隔，大小写敏感。* 与 ? 匹配单层，独立 ** 匹配任意层；匹配目录排除整支。最多 32 条，每条 UTF-8 最多 256 字节。不支持绝对路径、..、反斜线、字符类或否定。排除范围的既有曲目保留，不标记缺失。"},
			},
			"if":   map[string]any{"properties": map[string]any{"recursive": map[string]any{"const": true}}, "required": []string{"recursive"}},
			"then": map[string]any{"properties": map[string]any{"maxDepth": map[string]any{"minimum": 0}}},
			"else": map[string]any{"properties": map[string]any{"maxDepth": map[string]any{"const": 0}}},
		},
	}
}
