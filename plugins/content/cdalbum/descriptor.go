// Package cdalbum 提供 CD 专辑的作品资料和本地文件包组织能力。
package cdalbum

import (
	"github.com/fnMrRice/MusicFS-server/plugins/content/history"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
)

const TypeID = "cd_album"

// Descriptor 与普通音乐共用署名和发行编号，CD 专辑可以把音轨展示在曲目列表。
func Descriptor() contenttype.Descriptor {
	descriptor := music.Descriptor()
	descriptor.ID = TypeID
	descriptor.PluginID = "musicfs.plugin.content.cd_album"
	descriptor.Name = "CD专辑"
	descriptor.Description = "管理 CD 专辑、各碟音轨与随碟资料，可将专辑曲目加入普通曲目列表。"
	descriptor.Library = &contenttype.Library{Order: 10, Icon: "disc", TrackListOptional: true, SeparateHistory: true}
	return descriptor
}

func PackageOrganizer() contenttype.PackageOrganizer { return contenttype.DirectoryOrganizer{} }

func History(state contenttype.HistoryState) contenttype.History { return history.ByWork(state) }
