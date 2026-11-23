package driver

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func (c *Client) DailySongs(ctx context.Context, limit int) (online.DailySongsPage, error) {
	if limit < 1 || limit > MaxPageItems {
		return online.DailySongsPage{}, fault.Fail("INVALID_ARGUMENT", "Daily song limit must be between 1 and 50")
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return online.DailySongsPage{}, err
	}
	var response struct {
		Code *int `json:"code"`
		Data struct {
			Songs []neteaseSong `json:"dailySongs"`
		} `json:"data"`
	}
	if err := c.Get(ctx, "/api/v3/discovery/recommend/songs", nil, &response); err != nil {
		return online.DailySongsPage{}, err
	}
	if response.Code == nil {
		return online.DailySongsPage{}, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return online.DailySongsPage{}, err
	}
	if response.Data.Songs == nil {
		return online.DailySongsPage{}, upstreamInvalid()
	}
	// 平台按账号返回当天的完整列表，调用方的数量限制在映射时应用。
	songs := response.Data.Songs[:min(len(response.Data.Songs), limit)]
	page := online.DailySongsPage{Tracks: make([]online.Track, 0, len(songs)), ObservedAt: time.Now().UTC()}
	for _, song := range songs {
		track, err := song.track(true)
		if err != nil {
			return online.DailySongsPage{}, err
		}
		page.Tracks = append(page.Tracks, track)
	}
	return page, nil
}
