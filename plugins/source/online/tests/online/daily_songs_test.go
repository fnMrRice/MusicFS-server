package online_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestDailyPlaylistLeadsOrdinaryRecommendations(t *testing.T) {
	t.Parallel()
	const songsPath = "/api/v3/discovery/recommend/songs"
	const playlistsPath = "/api/personalized/playlist"
	calls := map[string]int{}
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case songsPath:
			return response(`{"code":200,"data":{"dailySongs":[{"id":41,"name":"Song","al":{"name":"Album","picUrl":"https://p1.music.126.net/cover.jpg"}}]}}`).RoundTrip(r)
		case playlistsPath:
			if r.URL.Query().Get("limit") != "1" {
				t.Fatal("Public request did not use remaining limit")
			}
			return response(`{"code":200,"result":[{"id":52,"name":"Playlist","copywriter":"Reason"}]}`).RoundTrip(r)
		default:
			return nil, fmt.Errorf("unexpected recommendation request: %s", r.URL.Path)
		}
	}))
	ctx := context.Background()
	page, err := c.PublicRecommendations(ctx, 2)
	if err != nil || !page.Personalized || len(page.Items) != 2 || page.Items[1].Position != 1 || page.Items[1].Reason != "Reason" || page.Items[1].Playlist.ExternalID != "52" {
		t.Fatal("unexpected recommendation page", page, err)
	}
	daily := page.Items[0].Playlist
	if daily.ExternalID != "daily-songs:12345" || daily.Title != "每日推荐歌曲" || daily.Writable || daily.TrackCount == nil || *daily.TrackCount != 1 || daily.CoverURL == nil || *daily.CoverURL != "https://p1.music.126.net/cover.jpg" {
		t.Fatal("invalid daily summary", daily)
	}
	if calls[songsPath] != 1 || calls[playlistsPath] != 1 {
		t.Fatal("unexpected upstream call counts", calls)
	}
}

func TestDailySongsValidatePlatformResponse(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"empty", `{"code":200,"data":{"dailySongs":[]}}`, ""},
		{"missing list", `{"code":200,"data":{}}`, "TEMPORARY_FAILURE"},
		{"missing status", `{"data":{"dailySongs":[]}}`, "TEMPORARY_FAILURE"},
		{"invalid track", `{"code":200,"data":{"dailySongs":[{"name":"Unknown"}]}}`, "TEMPORARY_FAILURE"},
		{"expired account", `{"code":301}`, "PLATFORM_AUTH_REQUIRED"},
		{"rate limit", `{"code":429}`, "RATE_LIMITED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, response(tc.body))
			page, err := c.ObservePlaylist(context.Background(), "daily-songs:12345")
			if tc.code != "" {
				expectCode(t, err, tc.code)
			} else if err != nil || page.Entries == nil || len(page.Entries) != 0 || page.ObservedAt.IsZero() || !page.Complete {
				t.Fatal("invalid empty daily song page", page, err)
			}
		})
	}
}

func TestRecommendationsKeepAvailableBranchAndRejectExpiredAccount(t *testing.T) {
	for _, tc := range []struct{ name, daily, public, wantID, wantError string }{
		{"daily unavailable", `{"code":500}`, `{"code":200,"result":[{"id":91,"name":"Public"}]}`, "91", ""},
		{"public unavailable", `{"code":200,"data":{"dailySongs":[]}}`, `{"code":500}`, "daily-songs:12345", ""},
		{"account expired", `{"code":200,"data":{"dailySongs":[]}}`, `{"code":301}`, "", "PLATFORM_AUTH_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/v3/discovery/recommend/songs" {
					return response(tc.daily).RoundTrip(r)
				}
				return response(tc.public).RoundTrip(r)
			}))
			page, err := c.PublicRecommendations(context.Background(), 2)
			if tc.wantError != "" {
				expectCode(t, err, tc.wantError)
				return
			}
			if err != nil || len(page.Items) != 1 || page.Items[0].Playlist.ExternalID != tc.wantID || page.Personalized != (tc.wantID == "daily-songs:12345") {
				t.Fatalf("unexpected fallback: %+v %v", page, err)
			}
		})
	}
}
