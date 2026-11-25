package audiotypes

import (
	"iter"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// NormalizeMatchName 统一全半角、大小写及连续空白，保留曲名中的版本与标点。
func NormalizeMatchName(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(name)), " "))
}

// 第一组保留 R&B、AC/DC 等短缩写及首尾符号。其余匹配才是分隔符。
// 带点的 feat./ft. 保留左词界，避免把 Left.Field 等完整艺人名切开。
var creditSeparators = regexp.MustCompile(`(?i)(\b[a-z0-9]{1,2}([&+/][a-z0-9]{1,2})+\b|^[&+/]+|[&+/]+$|[&+/]{2,})|\b(feat|ft)\.| (feat|featuring|ft) |[&+/×、;,]`)

// ArtistCreditMembers 按署名顺序返回成员，保留艺人名的大小写供平台检索。
func ArtistCreditMembers(credit string) iter.Seq[string] {
	credit = strings.Join(strings.Fields(norm.NFKC.String(credit)), " ")
	return func(yield func(string) bool) {
		start := 0
		for _, marker := range creditSeparators.FindAllStringSubmatchIndex(credit, -1) {
			if marker[2] >= 0 {
				continue
			}
			if name := strings.TrimSpace(credit[start:marker[0]]); name != "" && !yield(name) {
				return
			}
			start = marker[1]
		}
		if name := strings.TrimSpace(credit[start:]); name != "" {
			yield(name)
		}
	}
}

// 资料平台与来源标签都可能把多人写在同一项。核验时也保留未经拆分的整条署名。
func TrackCreditNames(credits []string) map[string]bool {
	names := make(map[string]bool)
	for _, credit := range credits {
		if full := NormalizeMatchName(credit); full != "" {
			names[full] = true
		}
		for member := range ArtistCreditMembers(credit) {
			names[strings.ToLower(member)] = true
		}
	}
	return names
}

// SameAlbumTrack 核对同一来源的两组标签。专辑名中的分碟编号、原声带或精选集后缀都属于发行证据。
func SameAlbumTrack(title, album string, artists []string, otherTitle, otherAlbum string, otherArtists []string) bool {
	album = NormalizeMatchName(album)
	return album != "" && album == NormalizeMatchName(otherAlbum) && SameTrackCredit(title, artists, otherTitle, otherArtists)
}

// SameTrackCredit 要求规范化后的原曲名相等，并且至少有一个完整署名相同。
func SameTrackCredit(title string, artists []string, otherTitle string, otherArtists []string) bool {
	title = NormalizeMatchName(title)
	if title == "" || title != NormalizeMatchName(otherTitle) {
		return false
	}
	names := TrackCreditNames(artists)
	for name := range TrackCreditNames(otherArtists) {
		if names[name] {
			return true
		}
	}
	return false
}
