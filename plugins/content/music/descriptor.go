// Package music 提供音乐作品的内置内容类型声明。
package music

import "github.com/fnMrRice/MusicFS-server/sdk/contenttype"

const (
	TypeID        = "music"
	PluginID      = "musicfs.plugin.content.music"
	PluginVersion = "0.1.0"
)

// Descriptor 每次返回独立声明；音乐作品的扫描和导入由独立能力处理。
func Descriptor() contenttype.Descriptor {
	return contenttype.Descriptor{
		ID: TypeID, PluginID: PluginID, PluginVersion: PluginVersion,
		Name:        "音乐",
		Description: "按作品、发行和碟片组织音乐，保留本地音频及附带资料。",
		CreditRoles: []contenttype.NamedValue{
			{ID: "artist", Name: "艺术家"},
			{ID: "composer", Name: "作曲"},
			{ID: "performer", Name: "演奏者"},
		},
		ExternalIDNamespaces: []contenttype.NamedValue{
			{ID: "catalog_number", Name: "发行目录编号"},
		},
		Views: []string{contenttype.ViewWorkList, contenttype.ViewWorkDetail},
	}
}
