package driver

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

const dailyPrefix = "daily-songs:"

func (c *Client) checkDailyID(id string) error {
	account, found := strings.CutPrefix(id, dailyPrefix)
	if !found || !numericID.MatchString(account) {
		return fault.Fail("INVALID_ARGUMENT", "Invalid daily playlist identifier")
	}
	if account != c.Secret.AccountID {
		return fault.Fail("NOT_FOUND", "Daily playlist was not found")
	}
	return nil
}

// 日推只属于已验证的当前账号。列表、观察和分页从相同的曲序生成摘要。
func (c *Client) observeDaily(ctx context.Context, id string) (PlaylistObservation, []Track, error) {
	if err := c.checkDailyID(id); err != nil {
		return PlaylistObservation{}, nil, err
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return PlaylistObservation{}, nil, err
	}
	var response struct {
		Code *int `json:"code"`
		Data struct {
			Songs []neteaseSong `json:"dailySongs"`
		} `json:"data"`
	}
	if err := c.Get(ctx, "/api/v3/discovery/recommend/songs", nil, &response); err != nil {
		return PlaylistObservation{}, nil, err
	}
	if response.Code == nil {
		return PlaylistObservation{}, nil, upstreamInvalid()
	}
	if err := c.BusinessError(*response.Code, 200); err != nil {
		return PlaylistObservation{}, nil, err
	}
	songs := response.Data.Songs
	if songs == nil || len(songs) > MaxPlaylistEntries {
		return PlaylistObservation{}, nil, upstreamInvalid()
	}
	count := int64(len(songs))
	playlist := Playlist{ExternalID: id, Title: "每日推荐歌曲", TrackCount: &count, SourceURL: online.ReferenceURL(policies.Netease().Playlists, id)}
	entries := make([]PlaylistEntry, len(songs))
	tracks := make([]Track, len(songs))
	for i, song := range songs {
		track, err := song.track(true)
		if err != nil {
			return PlaylistObservation{}, nil, err
		}
		if playlist.CoverURL == nil {
			playlist.CoverURL = track.CoverURL
		}
		tracks[i] = track
		entries[i] = PlaylistEntry{Position: i, ExternalTrackID: track.ExternalID}
	}
	return PlaylistObservation{Playlist: playlist, Entries: entries, Complete: true, Consistency: Unversioned, MetadataDigest: playlistDigest(playlist, entries), ObservedAt: time.Now().UTC()}, tracks, nil
}

func recommendationAccessLost(err error) bool {
	var failure *fault.Error
	return errors.As(err, &failure) && (failure.Code == "PLATFORM_AUTH_REQUIRED" || failure.Code == "ACCESS_DENIED" || failure.Code == "AUTH_REQUIRED")
}

// 为下一路查询或将已有结果返回宿主保留时间。显式取消仍立即终止整个请求。
func recommendationBudget(ctx context.Context, reserveRatio int) (context.Context, context.CancelFunc) {
	if deadline, ok := ctx.Deadline(); ok {
		return context.WithDeadline(ctx, deadline.Add(-time.Until(deadline)/time.Duration(reserveRatio)))
	}
	return context.WithCancel(ctx)
}

func (c *Client) PublicRecommendations(ctx context.Context, limit int) (RecommendationPage, error) {
	if limit < 1 || limit > MaxPageItems {
		return RecommendationPage{}, fault.Fail("INVALID_ARGUMENT", "Recommendation limit must be between 1 and 50")
	}
	if err := c.Require(ctx); err != nil {
		return RecommendationPage{}, err
	}
	page := RecommendationPage{Items: []Recommendation{}}
	dailyCtx, stop := recommendationBudget(ctx, 2)
	daily, _, dailyErr := c.observeDaily(dailyCtx, dailyPrefix+c.Secret.AccountID)
	stop()
	if err := ctx.Err(); err != nil {
		return RecommendationPage{}, err
	}
	if recommendationAccessLost(dailyErr) {
		return RecommendationPage{}, dailyErr
	}
	if dailyErr == nil {
		page.Items = append(page.Items, Recommendation{Playlist: daily.Playlist})
		page.Personalized = true
	}
	if remaining := limit - len(page.Items); remaining > 0 {
		publicCtx, stop := recommendationBudget(ctx, 10)
		items, err := c.publicPlaylists(publicCtx, remaining, len(page.Items))
		stop()
		if err != nil && (dailyErr != nil || recommendationAccessLost(err) || ctx.Err() != nil) {
			return RecommendationPage{}, err
		}
		if len(items) == 0 && dailyErr != nil {
			return RecommendationPage{}, dailyErr
		}
		page.Items = append(page.Items, items...)
	}
	page.ObservedAt = time.Now().UTC()
	page.ExpiresAt = page.ObservedAt.Add(2 * time.Minute)
	return page, nil
}
