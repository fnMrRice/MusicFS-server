package online

import (
	"net/url"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// 页面地址只用于提取平台编号；实际请求仍发往插件固定的目录 API。
func albumPageQuery(plugin, field string, q scraper.Query) (scraper.Query, error) {
	if strings.TrimSpace(q.SourceURL) == "" {
		return q, nil
	}
	invalid := func() (scraper.Query, error) {
		return q, fault.Fail("INVALID_ARGUMENT", "Use an album page from the selected metadata source")
	}
	if field != "albumDetails" && field != "albumTracks" && field != "artwork" {
		return invalid()
	}
	u, err := url.Parse(strings.TrimSpace(q.SourceURL))
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" || u.Port() != "" && u.Port() != "443" || len(q.SourceURL) > 4096 {
		return invalid()
	}
	host := strings.ToLower(u.Hostname())
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	var id, country string
	switch plugin {
	case "musicfs.scraper.musicbrainz", "musicfs.scraper.coverartarchive":
		if host != "musicbrainz.org" || len(parts) != 2 || parts[0] != "release" || !brainzID.MatchString(parts[1]) {
			return invalid()
		}
		id = strings.ToLower(parts[1])
	case "musicfs.scraper.bangumi":
		if host != "bgm.tv" && host != "bangumi.tv" && host != "chii.in" || len(parts) != 2 || parts[0] != "subject" {
			return invalid()
		}
		id, err = lookupNumericID(parts[1])
	case "musicfs.scraper.itunes":
		if host != "music.apple.com" && host != "itunes.apple.com" {
			return invalid()
		}
		if len(parts) > 0 && len(parts[0]) == 2 {
			country = strings.ToUpper(parts[0])
			if country[0] < 'A' || country[0] > 'Z' || country[1] < 'A' || country[1] > 'Z' {
				return invalid()
			}
			parts = parts[1:]
		}
		if len(parts) < 2 || len(parts) > 3 || parts[0] != "album" {
			return invalid()
		}
		id, err = lookupNumericID(strings.TrimPrefix(parts[len(parts)-1], "id"))
	default:
		return q, fault.Fail("UNSUPPORTED", "This metadata source does not support album page lookup")
	}
	if err != nil || id == "" || q.ExternalID != "" && !strings.EqualFold(q.ExternalID, id) {
		return invalid()
	}
	q.ExternalID = id
	if country != "" {
		q.Country = country
	}
	q.SourceURL = ""
	return q, nil
}
