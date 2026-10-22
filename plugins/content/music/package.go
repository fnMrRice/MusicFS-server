package music

import "github.com/fnMrRice/MusicFS-server/sdk/contenttype"

// PackageOrganizer 保留本地发行目录；CUE 在本阶段仍是附件，不生成分段音轨。
func PackageOrganizer() contenttype.PackageOrganizer { return contenttype.DirectoryOrganizer{} }
