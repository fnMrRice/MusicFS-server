// Package cdalbum 提供 CD 专辑的作品资料和本地文件包组织能力。
package cdalbum

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/history"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
)

const (
	TypeID   = "cd_album"
	PluginID = "musicfs.plugin.content.cd_album"
)

// Descriptor 声明 CD 专辑的署名、发行编号和曲目列表展示能力。
func Descriptor() contenttype.Descriptor {
	return contenttype.Descriptor{
		ID: TypeID, PluginID: PluginID, PluginVersion: bundledplugin.Version(PluginID),
		Name:        "CD专辑",
		Description: "管理 CD 专辑、各碟音轨与随碟资料，可将专辑曲目加入普通曲目列表。",
		CreditRoles: []contenttype.NamedValue{
			{ID: "artist", Name: "艺术家"},
			{ID: "composer", Name: "作曲"},
			{ID: "performer", Name: "演奏者"},
		},
		ExternalIDNamespaces: []contenttype.NamedValue{
			{ID: "catalog_number", Name: "发行目录编号"},
		},
		Views:   []string{contenttype.ViewWorkList, contenttype.ViewWorkDetail},
		Library: &contenttype.Library{Order: 10, Icon: "disc", TrackListOptional: true, SeparateHistory: true},
	}
}

func PackageOrganizer() contenttype.PackageOrganizer { return contenttype.DirectoryOrganizer{} }

func History(state contenttype.HistoryState) contenttype.History { return history.ByWork(state) }
