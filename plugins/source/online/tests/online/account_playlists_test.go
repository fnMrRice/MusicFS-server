package online_test

import (
	"context"
	"net/http"
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
			return response(`{"code":200,"more":true,"playlist":[{"id":91,"name":"My playlist","trackCount":2,"creator":{"nickname":"Fixture"}}]}`).RoundTrip(r)
		}
		if q.Get("offset") != "1" {
			t.Fatal("unexpected playlist page", r.URL)
		}
		return response(`{"code":200,"more":false,"playlist":[{"id":92,"name":"Saved playlist","trackCount":1}]}`).RoundTrip(r)
	}))
	ctx := context.Background()
	page, err := c.Playlists(ctx, online.PlaylistsRequest{Limit: 1})
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "91" || page.NextCursor == nil || online.ValidatePlaylists(online.Netease, page, 1) != nil {
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
	time.Sleep(1010 * time.Millisecond)
	page, err = c.Playlists(ctx, online.PlaylistsRequest{Limit: 1, Cursor: cursor})
	if err != nil || len(page.Items) != 1 || page.Items[0].ExternalID != "92" || page.NextCursor != nil {
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

func TestAccountPlaylistsRejectMalformedOrUnsafeResponses(t *testing.T) {
	for name, body := range map[string]string{
		"missing list":       `{"code":200,"more":false}`,
		"missing more":       `{"code":200,"playlist":[]}`,
		"empty continuation": `{"code":200,"more":true,"playlist":[]}`,
		"unsafe cover":       `{"code":200,"more":false,"playlist":[{"id":91,"name":"Name","coverImgUrl":"https://127.0.0.1/private"}]}`,
		"duplicate":          `{"code":200,"more":false,"playlist":[{"id":91,"name":"A"},{"id":91,"name":"B"}]}`,
		"oversized":          `{"code":200,"more":false,"playlist":[{"id":91,"name":"A"},{"id":92,"name":"B"},{"id":93,"name":"C"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, response(body))
			_, err := c.Playlists(context.Background(), online.PlaylistsRequest{Limit: 2})
			expectCode(t, err, "TEMPORARY_FAILURE")
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
