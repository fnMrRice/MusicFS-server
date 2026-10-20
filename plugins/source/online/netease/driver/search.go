package driver

import (
	"context"
	"net/url"
	"strconv"
)

func (c *Client) Search(ctx context.Context, request SearchRequest, offset int) ([]Track, bool, error) {
	var result struct {
		Code   *int `json:"code"`
		Result *struct {
			Songs     []neteaseSong `json:"songs"`
			SongCount *int64        `json:"songCount"`
			HasMore   *bool         `json:"hasMore"`
		} `json:"result"`
	}
	err := c.Get(ctx, "/api/search/get", url.Values{"s": {request.Query}, "type": {"1"}, "limit": {strconv.Itoa(request.Limit)}, "offset": {strconv.Itoa(offset)}}, &result)
	if err != nil {
		return nil, false, err
	}
	if result.Code == nil {
		return nil, false, upstreamInvalid()
	}
	if err = c.BusinessError(*result.Code, 200); err != nil {
		return nil, false, err
	}
	if result.Result == nil || result.Result.SongCount == nil || *result.Result.SongCount < 0 || len(result.Result.Songs) > request.Limit {
		return nil, false, upstreamInvalid()
	}
	tracks := make([]Track, 0, len(result.Result.Songs))
	for _, song := range result.Result.Songs {
		track, err := song.track(false)
		if err != nil {
			return nil, false, err
		}
		tracks = append(tracks, track)
	}
	more := int64(offset+len(tracks)) < *result.Result.SongCount
	if result.Result.HasMore != nil {
		more = more && *result.Result.HasMore
	}
	return tracks, more, nil
}
