package driver

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"html"
)

type qqSong struct {
	MID      string   `json:"songmid"`
	Name     string   `json:"songname"`
	Singer   []artist `json:"singer"`
	Album    string   `json:"albumname"`
	Interval *int64   `json:"interval"`
}

func (c *Client) Search(ctx context.Context, request online.SearchRequest, offset int) ([]online.Track, bool, error) {
	if err := c.Admit(ctx); err != nil {
		return nil, false, err
	}
	pageNumber := offset/request.Limit + 1
	var result struct {
		Code *int `json:"code"`
		Body *struct {
			Song *struct {
				List []struct {
					MID      string   `json:"mid"`
					Name     string   `json:"name"`
					Singer   []artist `json:"singer"`
					Album    album    `json:"album"`
					Interval *int64   `json:"interval"`
				} `json:"list"`
			} `json:"song"`
		} `json:"body"`
		Meta *struct {
			CurrentPage *int   `json:"curpage"`
			Total       *int64 `json:"sum"`
		} `json:"meta"`
	}
	// Desktop protocol reference: jsososo/QQMusicApi issue 160.
	err := c.RPC(ctx, c.Secret, "music.search.SearchCgiService", "DoSearchForQQMusicDesktop",
		map[string]any{"num_per_page": request.Limit, "page_num": pageNumber, "query": request.Query, "search_type": 0}, &result)
	if err != nil {
		return nil, false, err
	}
	if result.Code == nil {
		return nil, false, online.UpstreamInvalid()
	}
	if err = c.BusinessError(*result.Code, 0); err != nil {
		return nil, false, err
	}
	if result.Body == nil || result.Body.Song == nil || result.Meta == nil || result.Meta.Total == nil || *result.Meta.Total < 0 ||
		result.Meta.CurrentPage == nil || *result.Meta.CurrentPage != pageNumber || len(result.Body.Song.List) > request.Limit {
		return nil, false, online.UpstreamInvalid()
	}
	tracks := make([]online.Track, 0, len(result.Body.Song.List))
	for _, source := range result.Body.Song.List {
		song := qqSong{MID: source.MID, Name: source.Name, Singer: source.Singer, Album: source.Album.Name, Interval: source.Interval}
		track, err := song.track()
		if err != nil {
			return nil, false, err
		}
		tracks = append(tracks, track)
	}
	return tracks, int64(offset+len(tracks)) < *result.Meta.Total, nil
}

func (s qqSong) track() (online.Track, error) {
	if !songMID.MatchString(s.MID) || len(s.Singer) > 128 {
		return online.Track{}, online.UpstreamInvalid()
	}
	t := online.Track{ExternalID: s.MID, Artists: []string{}, PlaybackAvailability: "unknown", SourceURL: "https://y.qq.com/n/ryqq/songDetail/" + s.MID}
	var err error
	t.Title, err = online.TextValue(html.UnescapeString(s.Name), 4096)
	if err != nil || t.Title == "" {
		return online.Track{}, online.UpstreamInvalid()
	}
	t.Album, err = online.TextValue(html.UnescapeString(s.Album), 4096)
	if err != nil {
		return online.Track{}, err
	}
	for _, source := range s.Singer {
		name, err := online.TextValue(html.UnescapeString(source.Name), 4096)
		if err != nil {
			return online.Track{}, err
		}
		if name != "" {
			t.Artists = append(t.Artists, name)
		}
	}
	if s.Interval != nil && (*s.Interval < 0 || *s.Interval > 7*24*60*60) {
		return online.Track{}, online.UpstreamInvalid()
	}
	if s.Interval != nil && *s.Interval > 0 {
		value := *s.Interval * 1000
		t.DurationMs = &value
	}
	t.MetadataDigest = online.Digest(t)
	return t, nil
}
