package online

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

// QQMusic 查询 QQ 音乐网页的公开目录，只向固定平台地址发送资料请求。
func QQMusic(client *http.Client) scraper.Plugin {
	return newLookupPlugin(client, "qqmusic", "QQ 音乐", []string{"trackDetails", "albumDetails", "albumTracks", "artwork"}, searchQQMusic, "y.gtimg.cn")
}

func searchQQMusic(ctx context.Context, client *http.Client, field string, q scraper.Query) ([]scraper.Item, error) {
	album := field != "trackDetails"
	ids := []string{q.ExternalID}
	if q.ExternalID != "" {
		if !qqMusicMID.MatchString(q.ExternalID) {
			return nil, fault.Fail("INVALID_ARGUMENT", "Invalid QQ Music MID")
		}
	} else {
		name := q.Title
		if album {
			name = cmp.Or(q.Album, q.Title)
		}
		// 搜索建议把拼在标题后的歌手当作标题的一部分，容易漏掉同名发行版。
		term := cmp.Or(strings.TrimSpace(name), strings.TrimSpace(q.Artist))
		if term == "" {
			return nil, fault.Fail("INVALID_ARGUMENT", "A title, artist or QQ Music MID is required")
		}
		var err error
		ids, err = qqSuggestions(ctx, client, term, q.Artist, album)
		if err != nil {
			return nil, err
		}
	}
	items := []scraper.Item{}
	for _, mid := range ids {
		var item scraper.Item
		var err error
		if album {
			item, err = qqAlbum(ctx, client, mid, field)
		} else {
			item, err = qqTrack(ctx, client, mid)
		}
		if errors.Is(err, errCatalogMissing) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if item.ID != "" && (field != "albumTracks" || len(item.Tracks) > 0) && (field != "artwork" || item.ImageURL != "") {
			items = append(items, item)
		}
	}
	return items, nil
}

func qqSuggestions(ctx context.Context, client *http.Client, term, artist string, album bool) ([]string, error) {
	type suggestion struct{ MID, Singer string }
	type suggestions struct{ Itemlist []suggestion }
	var response struct {
		Code *int
		Data *struct{ Album, Song suggestions }
	}
	params := url.Values{"key": {term}, "format": {"json"}}
	if err := lookupJSON(ctx, client, "https://c.y.qq.com/splcloud/fcgi-bin/smartbox_new.fcg", params, nil, &response); err != nil {
		return nil, err
	}
	if response.Code == nil || *response.Code != 0 || response.Data == nil {
		return nil, errors.New("QQ Music search unavailable")
	}
	group := response.Data.Song
	if album {
		group = response.Data.Album
	}
	if artist = strings.TrimSpace(artist); artist != "" {
		sort.SliceStable(group.Itemlist, func(i, j int) bool {
			return strings.EqualFold(group.Itemlist[i].Singer, artist) && !strings.EqualFold(group.Itemlist[j].Singer, artist)
		})
	}
	seen := map[string]bool{}
	var ids []string
	for _, row := range group.Itemlist {
		if qqMusicMID.MatchString(row.MID) && !seen[row.MID] {
			seen[row.MID] = true
			ids = append(ids, row.MID)
			if len(ids) == 3 {
				break
			}
		}
	}
	return ids, nil
}

func qqCatalogRPC[T any](ctx context.Context, client *http.Client, module, method string, params map[string]any) (*T, error) {
	payload, err := json.Marshal(map[string]any{
		"comm":  map[string]int{"ct": 24, "cv": 0},
		"req_0": map[string]any{"module": module, "method": method, "param": params},
	})
	if err != nil {
		return nil, err
	}
	var response struct {
		Code   *int
		Result struct {
			Code *int
			Data *T
		} `json:"req_0"`
	}
	if err := lookupJSON(ctx, client, "https://u.y.qq.com/cgi-bin/musicu.fcg", url.Values{"data": {string(payload)}}, nil, &response); err != nil {
		return nil, err
	}
	if response.Code == nil || *response.Code != 0 || response.Result.Code == nil || *response.Result.Code != 0 || response.Result.Data == nil {
		return nil, errors.New("QQ Music catalog lookup unavailable")
	}
	return response.Result.Data, nil
}

type qqSinger struct{ Name, SingerName string }

func qqCredit(singers []qqSinger) string {
	names := make([]string, 0, len(singers))
	for _, singer := range singers {
		if name := strings.TrimSpace(html.UnescapeString(cmp.Or(singer.Name, singer.SingerName))); name != "" {
			names = append(names, name)
		}
	}
	return strings.Join(names, " / ")
}

type qqSongInfo struct {
	MID, Name, Title string
	Singer           []qqSinger
	Album            struct{ MID, PMID, Name, Title string }
	Interval         int
	Disc             int    `json:"index_cd"`
	Number           int    `json:"index_album"`
	Date             string `json:"time_public"`
}

func (s qqSongInfo) track() (scraper.Track, error) {
	title := strings.TrimSpace(html.UnescapeString(cmp.Or(s.Title, s.Name)))
	if !qqMusicMID.MatchString(s.MID) || title == "" || s.Interval < 0 || s.Disc < 0 || s.Number < 0 {
		return scraper.Track{}, errors.New("invalid QQ Music track metadata")
	}
	// QQ 的 index_cd 从零开始，index_album 已是展示用的曲目序号。
	return scraper.Track{ExternalID: s.MID, Title: title, Artist: qqCredit(s.Singer), Disc: s.Disc + 1, Number: s.Number, Duration: float64(s.Interval)}, nil
}

func qqTrack(ctx context.Context, client *http.Client, mid string) (scraper.Item, error) {
	type details struct {
		Song qqSongInfo `json:"track_info"`
	}
	data, err := qqCatalogRPC[details](ctx, client, "music.pf_song_detail_svr", "get_song_detail_yqq", map[string]any{"song_mid": mid})
	if err != nil {
		return scraper.Item{}, err
	}
	if data.Song.MID != mid {
		return scraper.Item{}, errCatalogMissing
	}
	track, err := data.Song.track()
	if err != nil {
		return scraper.Item{}, err
	}
	return scraper.Item{ID: mid, Title: track.Title, Artist: track.Artist, Duration: track.Duration,
		Album: html.UnescapeString(cmp.Or(data.Song.Album.Title, data.Song.Album.Name)), Date: data.Song.Date,
		ImageURL: qqCover(data.Song.Album.PMID), SourceURL: "https://y.qq.com/n/ryqq/songDetail/" + mid}, nil
}
