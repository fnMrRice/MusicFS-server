package matching

import (
	"slices"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"golang.org/x/text/unicode/norm"
)

// CatalogIdentity 只用于新入库的在线来源与用户现有录音的比较。
// NetEaseID 由来源命名空间或已解析的网易云媒体标签提供；普通来源 ID 不能填入。
type CatalogIdentity struct {
	Title, Album, NetEaseID string
	Artists                 []string
	DurationMs              int64
}

func catalogLabel(value string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFC.String(value)), " "))
}

// Keys 仅缩小候选范围。标点、版本说明和括号中的文字均参与比较。
func (s CatalogIdentity) Keys() []string {
	var keys []string
	if audiotypes.ValidNetEaseID(s.NetEaseID) {
		keys = append(keys, "netease:"+s.NetEaseID)
	}
	if title := catalogLabel(s.Title); title != "" {
		keys = append(keys, "title:"+title)
	}
	return keys
}

func catalogArtists(credits []string) []string {
	var artists []string
	for _, credit := range credits {
		for _, part := range strings.FieldsFunc(credit, func(ch rune) bool { return ch == '/' || ch == ';' || ch == '；' }) {
			if name := catalogLabel(part); name != "" {
				artists = append(artists, name)
			}
		}
	}
	slices.Sort(artists)
	return slices.Compact(artists)
}

func (s CatalogIdentity) Match(other CatalogIdentity) bool {
	if s.NetEaseID != "" && !audiotypes.ValidNetEaseID(s.NetEaseID) || other.NetEaseID != "" && !audiotypes.ValidNetEaseID(other.NetEaseID) {
		return false
	}
	// 已知时长差超过两秒时，即便标签的曲目编号相同，也保留独立来源。
	timed := s.DurationMs > 0 && other.DurationMs > 0
	if timed && max(s.DurationMs, other.DurationMs)-min(s.DurationMs, other.DurationMs) > 2000 {
		return false
	}
	if s.NetEaseID != "" && other.NetEaseID != "" {
		return s.NetEaseID == other.NetEaseID
	}
	title, album := catalogLabel(s.Title), catalogLabel(s.Album)
	if !timed || title == "" || album == "" || title != catalogLabel(other.Title) || album != catalogLabel(other.Album) {
		return false
	}
	artists := catalogArtists(s.Artists)
	return len(artists) > 0 && slices.Equal(artists, catalogArtists(other.Artists))
}
