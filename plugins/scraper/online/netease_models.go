package online

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

type neteaseArtist struct {
	ID                                        int64
	Name, PicURL, Img1v1URL, BriefDesc, Trans string
	Alias                                     []string
}

type neteaseAlbum struct {
	ID                                            int64
	Name, PicURL, Description, BriefDesc, Company string
	PublishTime                                   int64
	Artists                                       []neteaseArtist
	Artist                                        neteaseArtist
	Size                                          *int
}

type neteaseSong struct {
	ID               int64
	Name, CD         string
	No               int
	Duration, DT     int64
	Alias, Alia, Tns []string
	Artists, AR      []neteaseArtist
	Album, AL        neteaseAlbum
}

var lyricTimestamp = regexp.MustCompile(`(?m)^\s*\[\d{1,3}:\d{2}(?:[.:]\d{1,3})?\]`)
var neteaseTimeZone = time.FixedZone("UTC+8", 8*60*60)

// 平台搜索结果有时返回同一 CDN 的 HTTP 链接，交付图片前统一使用 HTTPS 并校验域名。
func neteaseImage(values ...string) string {
	for _, raw := range values {
		if strings.HasPrefix(raw, "http://") {
			raw = "https://" + strings.TrimPrefix(raw, "http://")
		}
		if scraper.AllowsImageURL(raw, []string{neteaseImages}) {
			return raw
		}
	}
	return ""
}

func neteaseNames(artists []neteaseArtist) string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		if name := strings.TrimSpace(artist.Name); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, " / ")
}

func neteaseAliases(primary string, groups ...[]string) []scraper.LocalizedTitle {
	var out []scraper.LocalizedTitle
	for _, aliases := range groups {
		for _, alias := range aliases {
			out = addPlatformName(out, primary, "und", alias)
		}
	}
	return out
}

func (r neteaseArtist) item() scraper.Item {
	id, name := numericTrackID(r.ID), strings.TrimSpace(r.Name)
	return scraper.Item{ID: id, Title: name, Artist: name, Description: r.BriefDesc,
		LocalizedArtistNames: neteaseAliases(name, r.Alias, []string{r.Trans}),
		SourceURL:            neteaseSite + "/artist?id=" + id, ImageURL: neteaseImage(r.PicURL, r.Img1v1URL)}
}

func (r neteaseAlbum) item() scraper.Item {
	id, title := numericTrackID(r.ID), strings.TrimSpace(r.Name)
	item := scraper.Item{ID: id, Title: title, Album: title, Artist: cmp.Or(neteaseNames(r.Artists), r.Artist.Name),
		Description: cmp.Or(r.Description, r.BriefDesc), Publisher: r.Company,
		SourceURL: neteaseSite + "/album?id=" + id, ImageURL: neteaseImage(r.PicURL)}
	if r.PublishTime != 0 {
		// 发行时间是北京时间当地日期，零点的时间戳在 UTC 仍属于前一天。
		item.Date = time.UnixMilli(r.PublishTime).In(neteaseTimeZone).Format(time.DateOnly)
	}
	return item
}

func (r neteaseSong) track() scraper.Track {
	title := strings.TrimSpace(r.Name)
	disc, _ := strconv.Atoi(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(r.CD)), "CD"))
	artists := r.AR
	if len(artists) == 0 {
		artists = r.Artists
	}
	return scraper.Track{ExternalID: numericTrackID(r.ID), Title: title, Artist: neteaseNames(artists),
		LocalizedTitles: neteaseAliases(title, r.Alias, r.Alia, r.Tns), Disc: max(0, disc), Number: max(0, r.No),
		Duration: float64(max(0, cmp.Or(r.DT, r.Duration))) / 1000}
}

func (r neteaseSong) item() scraper.Item {
	track := r.track()
	return scraper.Item{ID: track.ExternalID, Title: track.Title, Artist: track.Artist, LocalizedTitles: track.LocalizedTitles,
		Album: cmp.Or(r.AL.Name, r.Album.Name), Duration: track.Duration, ImageURL: neteaseImage(r.AL.PicURL, r.Album.PicURL),
		SourceURL: neteaseSite + "/song?id=" + track.ExternalID}
}
