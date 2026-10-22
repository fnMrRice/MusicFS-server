package asmr

import "github.com/fnMrRice/MusicFS-server/sdk/contenttype"

// PackageOrganizer 保留音声的多层目录与格式副本，不自动合并 BGM 或编码目录。
func PackageOrganizer() contenttype.PackageOrganizer { return contenttype.DirectoryOrganizer{} }
