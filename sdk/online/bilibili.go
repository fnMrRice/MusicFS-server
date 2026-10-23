package online

import (
	"regexp"
	"strings"
)

var bilibiliVideoID = regexp.MustCompile(`^video:(BV[0-9A-Za-z]{10}):([1-9][0-9]{0,19})$`)
var bilibiliUnavailableID = regexp.MustCompile(`^unavailable:[0-9]{1,5}:[1-9][0-9]{0,19}$`)

// BilibiliEntryID 允许收藏夹保留失效资源占位，但它们不能当作可播放曲目。
func BilibiliEntryID(id string) bool {
	return BilibiliTrackURL(id) != "" || bilibiliUnavailableID.MatchString(id)
}

// BilibiliTrackURL 只接受规范身份，不允许上游把任意链接混入普通 DTO。
func BilibiliTrackURL(id string) string {
	if match := bilibiliVideoID.FindStringSubmatch(id); match != nil {
		return "https://www.bilibili.com/video/" + match[1] + "?cid=" + match[2]
	}
	if value, ok := strings.CutPrefix(id, "audio:"); ok && numericID.MatchString(value) {
		return "https://www.bilibili.com/audio/au" + value
	}
	return ""
}

// BilibiliPlaylistURL 保持视频收藏夹与音频歌单的命名空间独立。
func BilibiliPlaylistURL(id string) string {
	if value, ok := strings.CutPrefix(id, "favorite:"); ok && numericID.MatchString(value) {
		return "https://www.bilibili.com/medialist/detail/ml" + value
	}
	if value, ok := strings.CutPrefix(id, "audio-menu:"); ok && numericID.MatchString(value) {
		return "https://www.bilibili.com/audio/am" + value
	}
	return ""
}

func bilibiliCapabilities() []Capability {
	result := Capabilities("")
	for i := range result {
		capability := &result[i]
		capability.Reason = "Not implemented by the anonymous Bilibili adapter"
		switch capability.Name {
		case "search.tracks", "search.playlists":
			capability.Supported = true
			capability.Reason = "Exact public IDs and links only; keyword search is unsupported"
		case "metadata.track", "track.details", "metadata.tracks.batch", "playlist.tracks", "playlist.observe":
			capability.Supported = true
			capability.Reason = "Anonymous public content; upstream availability is checked per request"
		case "stream.resolve":
			capability.Supported = true
			capability.Reason = "Public independent audio only; no membership or access restriction bypass"
		}
	}
	return result
}
