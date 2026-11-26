package driver

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

func (c *Client) AddPlaylistTrack(ctx context.Context, r online.PlaylistTrackAddition) error {
	if strings.HasPrefix(r.ExternalID, dailyPrefix) {
		if err := c.checkDailyID(r.ExternalID); err != nil {
			return err
		}
		return fault.Fail("ACCESS_DENIED", "Daily recommendations are read-only")
	}
	if !numericID.MatchString(r.ExternalID) || !numericID.MatchString(r.TrackID) {
		return fault.Fail("INVALID_ARGUMENT", "Invalid playlist or track identity")
	}
	// 重试前核对当前成员，即使上一次写入成功后连接断开也无需重复添加。
	observation, err := c.ObservePlaylist(ctx, r.ExternalID)
	if err != nil {
		return err
	}
	if !observation.Playlist.Writable {
		return fault.Fail("ACCESS_DENIED", "Only a playlist owned by this account can be updated")
	}
	for _, entry := range observation.Entries {
		if entry.ExternalTrackID == r.TrackID {
			return nil
		}
	}
	code, err := c.addTracks(ctx, r.ExternalID, []string{r.TrackID})
	if err == nil && code == 512 {
		// 平台对部分单曲请求返回 512，此时需要在同一请求内重复歌曲 ID。
		code, err = c.addTracks(ctx, r.ExternalID, []string{r.TrackID, r.TrackID})
	}
	if err != nil {
		return err
	}
	return c.BusinessError(code, 200)
}

func (c *Client) addTracks(ctx context.Context, playlistID string, tracks []string) (int, error) {
	if err := c.WaitForMetadata(ctx); err != nil {
		return 0, err
	}
	ids, err := json.Marshal(tracks)
	if err != nil {
		return 0, err
	}
	params := url.Values{"op": {"add"}, "pid": {playlistID}, "trackIds": {string(ids)}, "imme": {"true"}}
	var response struct {
		Code *int `json:"code"`
	}
	if err := c.Get(ctx, "/api/playlist/manipulate/tracks", params, &response); err != nil {
		return 0, err
	}
	if response.Code == nil {
		return 0, upstreamInvalid()
	}
	return *response.Code, nil
}
