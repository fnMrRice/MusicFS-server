package online_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestDailySongsFollowedByPublicRecommendations(t *testing.T) {
	t.Parallel()
	const songsPath = "/api/v3/discovery/recommend/songs"
	const playlistsPath = "/api/personalized/playlist"
	calls := map[string]int{}
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls[r.URL.Path]++
		switch r.URL.Path {
		case songsPath:
			return response(`{"code":200,"data":{"dailySongs":[{"id":41,"name":"Song"}]}}`).RoundTrip(r)
		case playlistsPath:
			return response(`{"code":200,"result":[{"id":52,"name":"Playlist"}]}`).RoundTrip(r)
		default:
			return nil, fmt.Errorf("unexpected recommendation request: %s", r.URL.Path)
		}
	}))
	ctx := context.Background()
	songs, err := c.DailySongs(ctx, 1)
	if err != nil || len(songs.Tracks) != 1 || songs.Tracks[0].ExternalID != "41" {
		t.Fatal("daily songs request failed", songs, err)
	}
	playlists, err := c.PublicRecommendations(ctx, 1)
	if err != nil || len(playlists.Items) != 1 || playlists.Items[0].Playlist.ExternalID != "52" {
		t.Fatal("public playlists request after daily songs failed", playlists, err)
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
			page, err := c.DailySongs(context.Background(), 20)
			if tc.code != "" {
				expectCode(t, err, tc.code)
			} else if err != nil || page.Tracks == nil || len(page.Tracks) != 0 || page.ObservedAt.IsZero() {
				t.Fatal("invalid empty daily song page", page, err)
			}
		})
	}
}
