package online

import (
	"context"
	"html"
)

type qqSong struct {
	MID      string   `json:"songmid"`
	Name     string   `json:"songname"`
	Singer   []artist `json:"singer"`
	Album    string   `json:"albumname"`
	Interval *int64   `json:"interval"`
}

func (c *Client) qqSearch(ctx context.Context, request SearchRequest, offset int) ([]Track, bool, error) {
	if err := c.admit(ctx); err != nil {
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
	err := c.qqAccountRPC(ctx, &c.account.secret, "music.search.SearchCgiService", "DoSearchForQQMusicDesktop",
		map[string]any{"num_per_page": request.Limit, "page_num": pageNumber, "query": request.Query, "search_type": 0}, &result)
	if err != nil {
		return nil, false, err
	}
	if result.Code == nil {
		return nil, false, upstreamInvalid()
	}
	if err = c.businessError(*result.Code, 0); err != nil {
		return nil, false, err
	}
	if result.Body == nil || result.Body.Song == nil || result.Meta == nil || result.Meta.Total == nil || *result.Meta.Total < 0 ||
		result.Meta.CurrentPage == nil || *result.Meta.CurrentPage != pageNumber || len(result.Body.Song.List) > request.Limit {
		return nil, false, upstreamInvalid()
	}
	tracks := make([]Track, 0, len(result.Body.Song.List))
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

func (s qqSong) track() (Track, error) {
	if !songMID.MatchString(s.MID) || len(s.Singer) > 128 {
		return Track{}, upstreamInvalid()
	}
	t := Track{ExternalID: s.MID, Artists: []string{}, PlaybackAvailability: "unknown", SourceURL: "https://y.qq.com/n/ryqq/songDetail/" + s.MID}
	var err error
	t.Title, err = textValue(html.UnescapeString(s.Name), 4096)
	if err != nil || t.Title == "" {
		return Track{}, upstreamInvalid()
	}
	t.Album, err = textValue(html.UnescapeString(s.Album), 4096)
	if err != nil {
		return Track{}, err
	}
	for _, source := range s.Singer {
		name, err := textValue(html.UnescapeString(source.Name), 4096)
		if err != nil {
			return Track{}, err
		}
		if name != "" {
			t.Artists = append(t.Artists, name)
		}
	}
	if s.Interval != nil && (*s.Interval < 0 || *s.Interval > 7*24*60*60) {
		return Track{}, upstreamInvalid()
	}
	if s.Interval != nil && *s.Interval > 0 {
		value := *s.Interval * 1000
		t.DurationMs = &value
	}
	t.MetadataDigest = digest(t)
	return t, nil
}
