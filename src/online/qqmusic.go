package online

import (
	"context"
	"html"
	"net/url"
	"strconv"
)

type qqSong struct {
	MID      string   `json:"songmid"`
	Name     string   `json:"songname"`
	Singer   []artist `json:"singer"`
	Album    string   `json:"albumname"`
	Interval *int64   `json:"interval"`
}

func (c *Client) qqSearch(ctx context.Context, request SearchRequest, offset int) ([]Track, bool, error) {
	pageNumber := offset/request.Limit + 1
	var response struct {
		Code *int `json:"code"`
		Data *struct {
			Song *struct {
				List        []qqSong `json:"list"`
				CurrentPage *int     `json:"curpage"`
				Total       *int64   `json:"totalnum"`
			} `json:"song"`
		} `json:"data"`
	}
	query := url.Values{"format": {"json"}, "n": {strconv.Itoa(request.Limit)}, "p": {strconv.Itoa(pageNumber)}, "w": {request.Query}, "cr": {"1"}, "g_tk": {"5381"}, "t": {"0"}}
	if err := c.get(ctx, "/soso/fcgi-bin/client_search_cp", query, &response); err != nil {
		return nil, false, err
	}
	if response.Code == nil {
		return nil, false, upstreamInvalid()
	}
	if err := c.businessError(*response.Code, 0); err != nil {
		return nil, false, err
	}
	if response.Data == nil || response.Data.Song == nil {
		return nil, false, upstreamInvalid()
	}
	song := response.Data.Song
	if song.Total == nil || *song.Total < 0 || song.CurrentPage == nil || *song.CurrentPage != pageNumber || len(song.List) > request.Limit {
		return nil, false, upstreamInvalid()
	}
	tracks := make([]Track, 0, len(song.List))
	for _, source := range song.List {
		track, err := source.track()
		if err != nil {
			return nil, false, err
		}
		tracks = append(tracks, track)
	}
	return tracks, int64(offset+len(tracks)) < *song.Total, nil
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
