package online_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/plugins/source/online/testkit"
)

func TestAccountPlaylistsUseValidatedAccountAndBoundPagination(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		q := r.URL.Query()
		if r.URL.Path != "/api/user/playlist" || q.Get("uid") != "12345" || q.Get("limit") != "1" || q.Get("includeVideo") != "true" {
			t.Fatal("playlist listing did not use the current account", r.URL)
		}
		if q.Get("offset") == "0" {
			return response(`{"code":200,"more":true,"playlist":[{"id":91,"name":"My playlist","trackCount":2,"creator":{"userId":12345,"nickname":"Fixture"},"coverImgUrl":"https://127.0.0.1/private"}]}`).RoundTrip(r)
		}
		if q.Get("offset") != "1" {
			t.Fatal("unexpected playlist page", r.URL)
		}
		return response(`{"code":200,"more":false,"playlist":[{"id":92,"userId":654,"name":"Saved playlist","trackCount":1}]}`).RoundTrip(r)
	}))
	ctx := context.Background()
	page, err := c.Playlists(ctx, online.PlaylistsRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "91" || !page.Items[0].Writable || page.Items[0].CoverURL != nil || page.NextCursor == nil || online.ValidatePlaylists(online.Netease, page, 1) != nil {
		t.Fatal("invalid first account page", page, err)
	}
	cursor := *page.NextCursor
	_, err = c.Playlists(ctx, online.PlaylistsRequest{Limit: 2, Cursor: cursor})
	expectCode(t, err, "INVALID_ARGUMENT")
	_, err = c.Search(ctx, online.SearchRequest{Kind: "playlists", Query: "My playlist", Limit: 1, Cursor: cursor})
	expectCode(t, err, "INVALID_ARGUMENT")
	if calls.Load() != 1 {
		t.Fatal("unbound cursor reached the upstream")
	}
	page, err = c.Playlists(ctx, online.PlaylistsRequest{Limit: 1, Cursor: cursor})
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "92" || page.Items[0].Writable || page.NextCursor != nil {
		t.Fatal("invalid next account page", page, err)
	}
	if _, err = c.Account(ctx, online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	_, err = c.Playlists(ctx, online.PlaylistsRequest{Limit: 1})
	expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
	time.Sleep(1010 * time.Millisecond)
	testkit.RestoreOnlineFixture(t, c, online.Netease, "user", "instance")
	_, err = c.Playlists(ctx, online.PlaylistsRequest{Limit: 1, Cursor: cursor})
	expectCode(t, err, "INVALID_ARGUMENT")
	if calls.Load() != 2 {
		t.Fatal("previous login cursor reached the upstream")
	}
}

func TestAccountPlaylistsPageAcrossPinnedFavorites(t *testing.T) {
	for _, tc := range []struct {
		name          string
		pinned        bool
		limit         int
		ordinaryCount int
		offsets       []int
		pages         [][]string
	}{
		{"multiple pages", true, 2, 5, []int{0, 1, 3}, [][]string{{"90", "101"}, {"102", "103"}, {"104", "105"}}},
		{"one item per page", true, 1, 2, []int{0, 0, 1}, [][]string{{"90"}, {"101"}, {"102"}}},
		{"overflow on upstream final page", true, 2, 2, []int{0, 1}, [][]string{{"90", "101"}, {"102"}}},
		{"only favorites", true, 1, 0, []int{0}, [][]string{{"90"}}},
		{"without favorites", false, 2, 3, []int{0, 2}, [][]string{{"101", "102"}, {"103"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var offsets []int
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
				if err != nil || offset < 0 || offset > tc.ordinaryCount {
					t.Fatalf("invalid upstream offset: %v (%v)", offset, err)
				}
				limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
				if err != nil || limit < 1 {
					t.Fatalf("invalid upstream limit: %v (%v)", limit, err)
				}
				offsets = append(offsets, offset)
				var rows []string
				if tc.pinned && offset == 0 {
					rows = append(rows, `{"id":90,"name":"Favorites","specialType":5}`)
				}
				end := min(offset+limit, tc.ordinaryCount)
				for index := offset; index < end; index++ {
					rows = append(rows, fmt.Sprintf(`{"id":%d,"name":"Playlist","specialType":0}`, 101+index))
				}
				body := fmt.Sprintf(`{"code":200,"more":%t,"playlist":[%s]}`, end < tc.ordinaryCount, strings.Join(rows, ","))
				return response(body).RoundTrip(r)
			}))
			request := online.PlaylistsRequest{Limit: tc.limit}
			for index, want := range tc.pages {
				page, err := c.Playlists(context.Background(), request)
				if err != nil {
					t.Fatalf("page %d: %v", index+1, err)
				}
				if err := online.ValidatePlaylists(online.Netease, page, tc.limit); err != nil {
					t.Fatalf("invalid page %d: %v", index+1, err)
				}
				var got []string
				for _, item := range page.Items {
					got = append(got, item.ExternalID)
				}
				if !slices.Equal(got, want) || (page.NextCursor != nil) != (index+1 < len(tc.pages)) {
					t.Fatalf("page %d: got IDs %v, has next %v; want IDs %v", index+1, got, page.NextCursor != nil, want)
				}
				if page.NextCursor != nil {
					request.Cursor = *page.NextCursor
				}
			}
			if !slices.Equal(offsets, tc.offsets) {
				t.Fatalf("upstream offsets: got %v, want %v", offsets, tc.offsets)
			}
		})
	}
}

func TestAccountPlaylistsRejectMalformedOrUnsafeResponses(t *testing.T) {
	for name, body := range map[string]string{
		"missing list":                       `{"code":200,"more":false}`,
		"missing more":                       `{"code":200,"playlist":[]}`,
		"empty continuation":                 `{"code":200,"more":true,"playlist":[]}`,
		"duplicate":                          `{"code":200,"more":false,"playlist":[{"id":91,"name":"A"},{"id":91,"name":"B"}]}`,
		"oversized":                          `{"code":200,"more":false,"playlist":[{"id":91,"name":"A"},{"id":92,"name":"B"},{"id":93,"name":"C"}]}`,
		"oversized after favorites":          `{"code":200,"more":false,"playlist":[{"id":90,"name":"Favorites","specialType":5},{"id":91,"name":"A"},{"id":92,"name":"B"},{"id":93,"name":"C"}]}`,
		"oversized with misplaced favorites": `{"code":200,"more":false,"playlist":[{"id":91,"name":"A"},{"id":90,"name":"Favorites","specialType":5},{"id":92,"name":"B"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, response(body))
			_, err := c.Playlists(context.Background(), online.PlaylistsRequest{Limit: 2})
			expectCode(t, err, "TEMPORARY_FAILURE")
		})
	}
}

func TestAccountPlaylistsExpireCursorWhenPinnedFavoritesChange(t *testing.T) {
	for name, second := range map[string]string{
		"replaced": `{"code":200,"more":false,"playlist":[{"id":93,"name":"Other","specialType":5},{"id":92,"name":"Regular"}]}`,
		"removed":  `{"code":200,"more":false,"playlist":[{"id":92,"name":"Regular"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := `{"code":200,"more":false,"playlist":[{"id":91,"name":"Favorites","specialType":5},{"id":92,"name":"Regular"}]}`
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				result := response(body)
				body = second
				return result.RoundTrip(r)
			}))
			request := online.PlaylistsRequest{Limit: 1}
			page, err := c.Playlists(context.Background(), request)
			if err != nil || page.NextCursor == nil {
				t.Fatalf("missing continuation after pinned favorites: %v", err)
			}
			request.Cursor = *page.NextCursor
			_, err = c.Playlists(context.Background(), request)
			expectCode(t, err, "CURSOR_EXPIRED")
		})
	}
}

func TestAccountPlaylistsRequireLoginAndSupportedCapability(t *testing.T) {
	t.Parallel()
	request := online.PlaylistsRequest{Limit: 20}
	c := anonymousScoped(t, online.Netease, response(`{}`))
	_, err := c.Playlists(context.Background(), request)
	expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
	c = scoped(t, online.QQMusic, response(`{}`))
	_, err = c.Playlists(context.Background(), request)
	expectCode(t, err, "UNSUPPORTED")
	c = scoped(t, online.Netease, response(`{"code":200,"more":false,"playlist":[]}`))
	page, err := c.Playlists(context.Background(), request)
	if err != nil || page.Items == nil || len(page.Items) != 0 || page.NextCursor != nil || online.ValidatePlaylists(online.Netease, page, 20) != nil {
		t.Fatal("legitimate empty account list failed", page, err)
	}
	page.Items = nil
	if online.ValidatePlaylists(online.Netease, page, 20) == nil {
		t.Fatal("nil list accepted by host")
	}
}

func TestAccountPlaylistRequestIsCanceledOnLogout(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	}))
	result := make(chan error, 1)
	go func() { _, err := c.Playlists(context.Background(), online.PlaylistsRequest{Limit: 1}); result <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("playlist request did not start")
	}
	if _, err := c.Account(context.Background(), online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
	case <-time.After(time.Second):
		t.Fatal("logout did not cancel playlist request")
	}
}
