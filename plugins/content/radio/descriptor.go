// Package radio 提供电台节目的作品资料和本地文件包组织能力。
package radio

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/history"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
)

const TypeID = "radio"

func Descriptor() contenttype.Descriptor {
	return contenttype.Descriptor{
		ID: TypeID, PluginID: "musicfs.plugin.content.radio", PluginVersion: "0.1.0",
		Name: "电台", Description: "按节目整理电台录音、分集和附件。",
		CreditRoles: []contenttype.NamedValue{{ID: "host", Name: "主持人"}, {ID: "guest", Name: "嘉宾"}},
		Views:       []string{contenttype.ViewWorkList, contenttype.ViewWorkDetail},
		Library:     &contenttype.Library{Order: 30, Icon: "radio", SeparateHistory: true},
	}
}

func PackageOrganizer() contenttype.PackageOrganizer { return contenttype.DirectoryOrganizer{} }

func History(state contenttype.HistoryState) contenttype.History { return history.ByEpisode(state) }
