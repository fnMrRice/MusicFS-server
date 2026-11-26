package driver

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type neteasePlaylist struct {
	ID          json.Number `json:"id"`
	UserID      json.Number `json:"userId"`
	Name        string      `json:"name"`
	TrackCount  *int64      `json:"trackCount"`
	CoverImgURL string      `json:"coverImgUrl"`
	PicURL      string      `json:"picUrl"`
	Creator     *struct {
		UserID   json.Number `json:"userId"`
		Nickname string      `json:"nickname"`
	} `json:"creator"`
}

func (p neteasePlaylist) ownedBy(accountID string) bool {
	id := p.UserID.String()
	if p.Creator != nil && p.Creator.UserID != "" {
		if id != "" && id != p.Creator.UserID.String() {
			return false
		}
		id = p.Creator.UserID.String()
	}
	return id != "" && id == accountID
}

func (p neteasePlaylist) summary() (Playlist, error) {
	if !numericID.MatchString(p.ID.String()) || (p.TrackCount != nil && *p.TrackCount < 0) {
		return Playlist{}, upstreamInvalid()
	}
	name, err := textValue(p.Name, 4096)
	if err != nil || name == "" {
		return Playlist{}, upstreamInvalid()
	}
	image := p.CoverImgURL
	if image == "" {
		image = p.PicURL
	}
	// 可选封面不符合展示策略时省略，保留歌单身份和名称。
	cover, _ := imageURL(image)
	var creator *string
	if p.Creator != nil {
		value, err := textValue(p.Creator.Nickname, 4096)
		if err != nil {
			return Playlist{}, err
		}
		if value != "" {
			creator = &value
		}
	}
	return Playlist{ExternalID: p.ID.String(), Title: name, SourceURL: "https://music.163.com/playlist?id=" + p.ID.String(), TrackCount: p.TrackCount, CoverURL: cover, Creator: creator}, nil
}

// 公共歌单仅有一页，位置接在账号日推之后。
func (c *Client) publicPlaylists(ctx context.Context, limit, start int) ([]Recommendation, error) {
	var response struct {
		Code   *int `json:"code"`
		Result *[]struct {
			neteasePlaylist
			Reason string `json:"copywriter"`
		} `json:"result"`
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return nil, err
	}
	if err := c.Get(ctx, "/api/personalized/playlist", url.Values{"limit": {strconv.Itoa(limit)}, "total": {"true"}, "n": {"1"}}, &response); err != nil {
		return nil, err
	}
	if response.Code == nil {
		return nil, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return nil, err
	}
	if response.Result == nil || len(*response.Result) > limit {
		return nil, upstreamInvalid()
	}
	items := make([]Recommendation, 0, len(*response.Result))
	for index, source := range *response.Result {
		playlist, err := source.summary()
		if err != nil {
			return nil, err
		}
		reason, err := textValue(source.Reason, 4096)
		if err != nil {
			return nil, err
		}
		items = append(items, Recommendation{Playlist: playlist, Position: start + index, Reason: reason})
	}
	return items, nil
}

// ObservePlaylist never calls track-detail or audio endpoints for its entries.
// Completeness describes only this response, not a remotely fixed snapshot.
func (c *Client) ObservePlaylist(ctx context.Context, externalID string) (PlaylistObservation, error) {
	if err := c.Require(ctx); err != nil {
		return PlaylistObservation{}, err
	}
	if strings.HasPrefix(externalID, dailyPrefix) {
		o, _, err := c.observeDaily(ctx, externalID)
		return o, err
	}
	if !numericID.MatchString(externalID) {
		return PlaylistObservation{}, domain.Fail("INVALID_ARGUMENT", "Playlist identifier is invalid")
	}
	var response struct {
		Code     *int `json:"code"`
		Playlist *struct {
			neteasePlaylist
			Privacy  *int `json:"privacy"`
			TrackIDs *[]struct {
				ID json.Number `json:"id"`
			} `json:"trackIds"`
		} `json:"playlist"`
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return PlaylistObservation{}, err
	}
	if err := c.Get(ctx, "/api/v6/playlist/detail", url.Values{"id": {externalID}, "n": {"1"}, "s": {"0"}}, &response); err != nil {
		return PlaylistObservation{}, err
	}
	if response.Code == nil {
		return PlaylistObservation{}, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return PlaylistObservation{}, err
	}
	p := response.Playlist
	if p == nil {
		return PlaylistObservation{}, domain.Fail("NOT_FOUND", "Public playlist was not found")
	}
	owned := p.ownedBy(c.Secret.AccountID)
	if !owned && (p.Privacy == nil || *p.Privacy != 0) {
		return PlaylistObservation{}, domain.Fail("ACCESS_DENIED", "This account cannot access this private playlist")
	}
	if p.ID.String() != externalID || p.TrackIDs == nil || p.TrackCount == nil || len(*p.TrackIDs) > MaxPlaylistEntries {
		return PlaylistObservation{}, upstreamInvalid()
	}
	playlist, err := p.summary()
	if err != nil {
		return PlaylistObservation{}, err
	}
	playlist.Writable = owned
	observation := PlaylistObservation{Playlist: playlist, Entries: []PlaylistEntry{}, Complete: *p.TrackCount == int64(len(*p.TrackIDs)), Consistency: Unversioned, ObservedAt: time.Now().UTC()}
	for index, entry := range *p.TrackIDs {
		if !numericID.MatchString(entry.ID.String()) {
			return PlaylistObservation{}, upstreamInvalid()
		}
		observation.Entries = append(observation.Entries, PlaylistEntry{Position: index, ExternalTrackID: entry.ID.String()})
	}
	observation.MetadataDigest = playlistDigest(playlist, observation.Entries)
	return observation, nil
}
