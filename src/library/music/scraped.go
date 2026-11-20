package music

// AlbumScrapedField 限定专辑可保存的在线资料，避免误用曲目或歌手字段。
func AlbumScrapedField(field string) bool {
	return field == "albumDetails" || field == "albumTracks" || field == "artwork"
}
