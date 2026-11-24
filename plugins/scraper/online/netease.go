package online

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

const neteaseSite = "https://music.163.com"
const neteaseImages = "*.music.126.net"

// NetEase 按用户所选字段查询网易云公开目录和歌词。
func NetEase(client *http.Client) scraper.Plugin {
	return newLookupPlugin(client, "netease", "网易云音乐", []string{
		"albumDetails", "albumTracks", "artwork", "trackDetails", "lyrics", "artistDetails", "artistImage",
	}, searchNetEase, neteaseImages)
}

type neteaseMatches struct {
	Albums  []neteaseAlbum
	Songs   []neteaseSong
	Artists []neteaseArtist
}

type neteaseResponse struct {
	Code                 int
	Result               *neteaseMatches
	Album                neteaseAlbum
	Songs                []neteaseSong
	Artist               neteaseArtist
	Nolyric, Uncollected bool
	Lrc                  struct{ Lyric string }
}

func neteaseGet(ctx context.Context, client *http.Client, path string, params url.Values) (neteaseResponse, error) {
	var response neteaseResponse
	if err := lookupJSON(ctx, client, neteaseSite+path, params, nil, &response); err != nil {
		return response, err
	}
	switch response.Code {
	case 200:
		return response, nil
	case 404:
		return response, errCatalogMissing
	case 429:
		return response, fault.Fail("RATE_LIMITED", "NetEase catalog rate limit reached")
	default:
		return response, fault.Fail("SOURCE_UNAVAILABLE", fmt.Sprintf("NetEase catalog returned status %d", response.Code))
	}
}

func searchNetEase(ctx context.Context, client *http.Client, field string, q scraper.Query) ([]scraper.Item, error) {
	var candidates []scraper.Item
	if q.ExternalID != "" {
		id, err := lookupNumericID(q.ExternalID)
		if err != nil {
			return nil, err
		}
		candidates = []scraper.Item{{ID: id}}
	} else {
		var err error
		candidates, err = neteaseSuggestions(ctx, client, field, q)
		if err != nil {
			return nil, err
		}
	}

	items := []scraper.Item{}
	seen := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if candidate.ID == "" || seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		item, err := neteaseCandidate(ctx, client, field, candidate, q.ExternalID != "")
		if errors.Is(err, errCatalogMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if item.Title == "" || (field == "artwork" || field == "artistImage") && item.ImageURL == "" ||
			field == "albumTracks" && len(item.Tracks) == 0 || field == "lyrics" && item.Lyrics == "" {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func neteaseSuggestions(ctx context.Context, client *http.Client, field string, q scraper.Query) ([]scraper.Item, error) {
	kind, term := "10", strings.TrimSpace(cmp.Or(q.Album, q.Title)+" "+q.Artist)
	switch field {
	case "trackDetails", "lyrics":
		kind, term = "1", strings.TrimSpace(q.Title+" "+q.Artist)
	case "artistDetails", "artistImage":
		kind, term = "100", strings.TrimSpace(cmp.Or(q.Artist, q.Title))
	}
	if term == "" {
		return nil, fault.Fail("INVALID_ARGUMENT", "NetEase search term or catalog ID required")
	}
	limit := 3
	if field == "artwork" || field == "artistImage" {
		limit = 8
	}
	response, err := neteaseGet(ctx, client, "/api/search/get", url.Values{"s": {term}, "type": {kind}, "limit": {strconv.Itoa(limit)}, "offset": {"0"}})
	if err != nil {
		return nil, err
	}
	matches := response.Result
	if matches == nil || len(matches.Albums)+len(matches.Artists)+len(matches.Songs) > limit {
		return nil, errors.New("invalid NetEase search result")
	}
	var items []scraper.Item
	switch kind {
	case "10":
		for _, album := range matches.Albums {
			items = append(items, album.item())
		}
	case "100":
		for _, artist := range matches.Artists {
			items = append(items, artist.item())
		}
	case "1":
		for _, song := range matches.Songs {
			items = append(items, song.item())
		}
	}
	return items, nil
}

// 名称搜索只返回摘要；选用详情时须读取同一编号的完整记录。
func neteaseCandidate(ctx context.Context, client *http.Client, field string, item scraper.Item, exact bool) (scraper.Item, error) {
	id := item.ID
	switch field {
	case "albumDetails", "albumTracks", "artwork":
		if field != "artwork" || exact {
			return neteaseAlbumByID(ctx, client, field, id)
		}
	case "artistDetails", "artistImage":
		if field == "artistDetails" || exact {
			response, err := neteaseGet(ctx, client, "/api/artist/"+id, nil)
			if err != nil {
				return item, err
			}
			item = response.Artist.item()
			if item.ID != id {
				return item, errors.New("unexpected NetEase artist identity")
			}
		}
	case "trackDetails", "lyrics":
		if field == "trackDetails" || exact {
			response, err := neteaseGet(ctx, client, "/api/v3/song/detail", url.Values{"c": {`[{"id":` + id + `}]`}})
			if err != nil {
				return item, err
			}
			if len(response.Songs) == 0 {
				return item, errCatalogMissing
			}
			if len(response.Songs) != 1 || numericTrackID(response.Songs[0].ID) != id {
				return item, errors.New("unexpected NetEase song identity")
			}
			item = response.Songs[0].item()
		}
		if field == "lyrics" {
			response, err := neteaseGet(ctx, client, "/api/song/lyric", url.Values{"id": {id}, "lv": {"-1"}})
			if err != nil {
				return item, err
			}
			if !response.Nolyric && !response.Uncollected && strings.TrimSpace(response.Lrc.Lyric) != "" {
				item.Lyrics, item.Format = response.Lrc.Lyric, "text"
				if lyricTimestamp.MatchString(item.Lyrics) {
					item.Format = "lrc"
				}
			}
		}
	}
	return item, nil
}
func neteaseAlbumByID(ctx context.Context, client *http.Client, field, id string) (scraper.Item, error) {
	response, err := neteaseGet(ctx, client, "/api/v1/album/"+id, nil)
	if err != nil {
		return scraper.Item{}, err
	}
	item := response.Album.item()
	if item.ID != id || item.Title == "" {
		return scraper.Item{}, errors.New("unexpected NetEase album identity")
	}
	if field == "albumTracks" {
		if response.Album.Size == nil || *response.Album.Size != len(response.Songs) || len(response.Songs) > 2000 {
			return scraper.Item{}, errors.New("incomplete or oversized NetEase album track list")
		}
		type appearance struct {
			id           int64
			disc, number int
		}
		seen := make(map[appearance]bool, len(response.Songs))
		for _, song := range response.Songs {
			track := song.track()
			key := appearance{id: song.ID, disc: track.Disc, number: track.Number}
			if track.ExternalID == "" || track.Title == "" || seen[key] ||
				song.AL.ID != 0 && song.AL.ID != response.Album.ID || song.Album.ID != 0 && song.Album.ID != response.Album.ID {
				return scraper.Item{}, errors.New("invalid NetEase album track")
			}
			seen[key] = true
			item.Tracks = append(item.Tracks, track)
		}
	}
	return item, nil
}
