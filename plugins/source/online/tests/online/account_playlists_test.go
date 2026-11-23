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
		if r.URL.Path != "/api/user/playlist" || q.Get("uid") != "12345" || q.Get("limit") != strconv.Itoa(online.MaxPageItems) || q.Get("includeVideo") != "true" {
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
		upstreamExtra int
		offsets       []int
		pageSizes     []int
	}{
		{"39 playlists in final batch", true, 30, 38, 0, []int{0, 0}, []int{30, 9}},
		{"pinned across upstream batches", true, 30, 53, 0, []int{0, 0, 50}, []int{30, 21, 3}},
		{"upstream overfetch with more batches", true, 30, 65, 10, []int{0, 0, 50}, []int{30, 21, 15}},
		{"oversized final batch", false, 30, 61, 20, []int{0, 0, 0}, []int{30, 30, 1}},
		{"one item per page", true, 1, 2, 0, []int{0, 0, 0}, []int{1, 1, 1}},
		{"only favorites", true, 1, 0, 0, []int{0}, []int{1}},
		{"without favorites across batches", false, 30, 53, 0, []int{0, 0, 50}, []int{30, 20, 3}},
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
				if err != nil || limit != online.MaxPageItems {
					t.Fatalf("invalid upstream limit: %v (%v)", limit, err)
				}
				offsets = append(offsets, offset)
				var rows []string
				if tc.pinned && offset == 0 {
					rows = append(rows, `{"id":90,"name":"Favorites","specialType":5}`)
				}
				end := min(offset+limit+tc.upstreamExtra, tc.ordinaryCount)
				for index := offset; index < end; index++ {
					rows = append(rows, fmt.Sprintf(`{"id":%d,"name":"Playlist","specialType":0}`, 101+index))
				}
				body := fmt.Sprintf(`{"code":200,"more":%t,"playlist":[%s]}`, end < tc.ordinaryCount, strings.Join(rows, ","))
				return response(body).RoundTrip(r)
			}))
			request := online.PlaylistsRequest{Limit: tc.limit}
			var expectedIDs []string
			if tc.pinned {
				expectedIDs = append(expectedIDs, "90")
			}
			for index := range tc.ordinaryCount {
				expectedIDs = append(expectedIDs, strconv.Itoa(101+index))
			}
			var got []string
			for index, wantSize := range tc.pageSizes {
				page, err := c.Playlists(context.Background(), request)
				if err != nil {
					t.Fatalf("page %d: %v", index+1, err)
				}
				if err := online.ValidatePlaylists(online.Netease, page, tc.limit); err != nil {
					t.Fatalf("invalid page %d: %v", index+1, err)
				}
				for _, item := range page.Items {
					got = append(got, item.ExternalID)
				}
				if len(page.Items) != wantSize || (page.NextCursor != nil) != (index+1 < len(tc.pageSizes)) {
					t.Fatalf("page %d: got %d items and has next %v, want %d items", index+1, len(page.Items), page.NextCursor != nil, wantSize)
				}
				if page.NextCursor != nil {
					request.Cursor = *page.NextCursor
				}
			}
			if !slices.Equal(got, expectedIDs) {
				t.Fatalf("playlist sequence: got %v, want %v", got, expectedIDs)
			}
			if !slices.Equal(offsets, tc.offsets) {
				t.Fatalf("upstream offsets: got %v, want %v", offsets, tc.offsets)
			}
		})
	}
}

func TestAccountPlaylistsRejectMalformedOrUnsafeResponses(t *testing.T) {
	for name, body := range map[string]string{
		"missing list":       `{"code":200,"more":false}`,
		"missing more":       `{"code":200,"playlist":[]}`,
		"empty continuation": `{"code":200,"more":true,"playlist":[]}`,
		"duplicate":          `{"code":200,"more":false,"playlist":[{"id":91,"name":"A"},{"id":91,"name":"B"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, response(body))
			_, err := c.Playlists(context.Background(), online.PlaylistsRequest{Limit: 2})
			expectCode(t, err, "TEMPORARY_FAILURE")
		})
	}
}

func TestAccountPlaylistsRejectCollectionsAboveSupportedLimit(t *testing.T) {
	t.Parallel()
	rows := make([]string, online.MaxPlaylistEntries+1)
	for index := range rows {
		rows[index] = fmt.Sprintf(`{"id":%d,"name":"Regular"}`, 101+index)
	}
	body := `{"code":200,"more":false,"playlist":[` + strings.Join(rows, ",") + `]}`
	c := scoped(t, online.Netease, response(body))
	_, err := c.Playlists(context.Background(), online.PlaylistsRequest{Limit: 30})
	expectCode(t, err, "UNSUPPORTED")
}

func TestAccountPlaylistsExpireCursorWhenBoundaryChanges(t *testing.T) {
	for _, tc := range []struct {
		name   string
		limit  int
		second string
	}{
		{"replaced favorites", 1, `{"code":200,"more":false,"playlist":[{"id":93,"name":"Other","specialType":5},{"id":92,"name":"Regular"}]}`},
		{"removed favorites", 1, `{"code":200,"more":false,"playlist":[{"id":92,"name":"Regular"}]}`},
		{"replaced regular playlist", 2, `{"code":200,"more":false,"playlist":[{"id":91,"name":"Favorites","specialType":5},{"id":94,"name":"Changed"},{"id":93,"name":"Third"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"code":200,"more":false,"playlist":[{"id":91,"name":"Favorites","specialType":5},{"id":92,"name":"Regular"},{"id":93,"name":"Third"}]}`
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				result := response(body)
				body = tc.second
				return result.RoundTrip(r)
			}))
			request := online.PlaylistsRequest{Limit: tc.limit}
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
