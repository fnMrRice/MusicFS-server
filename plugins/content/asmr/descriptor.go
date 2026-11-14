// Package asmr 提供本地音声作品的内置内容类型声明。
package asmr

import "github.com/fnMrRice/MusicFS-server/sdk/contenttype"

const (
	TypeID        = "asmr"
	PluginID      = "musicfs.plugin.content.asmr"
	PluginVersion = "0.1.0"
)

// Descriptor 每次返回独立声明；此声明不解析或改写外部作品编号。
func Descriptor() contenttype.Descriptor {
	return contenttype.Descriptor{
		ID: TypeID, PluginID: PluginID, PluginVersion: PluginVersion,
		Name:        "ASMR",
		Library:     &contenttype.Library{Order: 20, Icon: "headphones"},
		Description: "按作品包组织本地音声，保留多层音轨目录、内容变体和附件。",
		CreditRoles: []contenttype.NamedValue{
			{ID: "circle", Name: "社团"},
			{ID: "voice_actor", Name: "声优"},
			{ID: "scenario", Name: "脚本"},
			{ID: "illustrator", Name: "插画"},
		},
		ExternalIDNamespaces: []contenttype.NamedValue{
			{ID: "dlsite", Name: "音声商店作品编号"},
		},
		Views: []string{contenttype.ViewWorkList, contenttype.ViewWorkDetail},
	}
}
