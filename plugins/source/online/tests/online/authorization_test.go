package online_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestAllOnlineOperationsRejectAnonymousWithoutNetwork(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		t.Run(platform, func(t *testing.T) {
			var calls atomic.Int32
			c := anonymousScoped(t, platform, roundTrip(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, nil }))
			ctx := context.Background()
			operations := []func() error{
				func() error { _, e := c.Search(ctx, online.SearchRequest{Query: "x", Limit: 1}); return e },
				func() error { _, e := c.Metadata(ctx, "42"); return e },
				func() error { _, e := c.MetadataBatch(ctx, []string{"42"}); return e },
				func() error { _, e := c.ObservePlaylist(ctx, "42"); return e },
				func() error {
					_, e := c.PlaylistTracks(ctx, online.PlaylistTracksRequest{ExternalID: "42", Limit: 1})
					return e
				},
				func() error { _, e := c.PublicRecommendations(ctx, 1); return e },
				func() error { _, e := c.Album(ctx, "42"); return e },
				func() error { _, e := c.Artist(ctx, "42"); return e },
				func() error {
					_, e := c.AlbumTracks(ctx, online.EntityPageRequest{ExternalID: "42", Limit: 1})
					return e
				},
				func() error {
					_, e := c.ArtistAlbums(ctx, online.EntityPageRequest{ExternalID: "42", Limit: 1})
					return e
				},
				func() error {
					_, e := c.ArtistTracks(ctx, online.EntityPageRequest{ExternalID: "42", Limit: 1})
					return e
				},
				func() error { return c.Resolve(ctx, "42") },
			}
			for _, operation := range operations {
				expectCode(t, operation(), "PLATFORM_AUTH_REQUIRED")
			}
			secret, _ := json.Marshal(map[string]any{"platform": platform, "userId": "user", "instanceId": "instance", "accountId": "12345"})
			if _, e := c.Account(ctx, online.AccountRequest{Action: "restore", Secret: secret}); e != nil {
				t.Fatal(e)
			}
			for _, operation := range operations {
				expectCode(t, operation(), "PLATFORM_AUTH_REQUIRED")
			}
			if calls.Load() != 0 {
				t.Fatal("unauthenticated operation reached the platform")
			}
		})
	}
}

func TestLogoutCancelsBusinessRequestAndDiscardsLateResult(t *testing.T) {
	started := make(chan struct{})
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return response(neSearch).RoundTrip(r) // Simulate an upstream ignoring cancellation.
	}))
	done := make(chan error, 1)
	go func() { _, e := c.Search(context.Background(), online.SearchRequest{Query: "x", Limit: 1}); done <- e }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("query did not start")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, e := c.Account(ctx, online.AccountRequest{Action: "logout"}); e != nil {
		t.Fatal(e)
	}
	expectCode(t, <-done, "PLATFORM_AUTH_REQUIRED")
	_, e := c.Search(ctx, online.SearchRequest{Query: "x", Limit: 1})
	expectCode(t, e, "PLATFORM_AUTH_REQUIRED")
}

func TestExpiredSessionRejectsFurtherBusinessRequests(t *testing.T) {
	var calls atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(`{"code":301}`).RoundTrip(r)
	}))
	for range 2 {
		_, e := c.Search(context.Background(), online.SearchRequest{Query: "x", Limit: 1})
		expectCode(t, e, "PLATFORM_AUTH_REQUIRED")
	}
	state, e := c.Account(context.Background(), online.AccountRequest{Action: "state"})
	if e != nil || state.View.State != "expired" || len(state.Secret) != 0 || calls.Load() != 1 {
		t.Fatal("expired account remained usable")
	}
}

func TestSearchCursorCannotCrossLoginGenerations(t *testing.T) {
	c := scoped(t, online.Netease, response(neSearch))
	ctx := context.Background()
	request := online.SearchRequest{Query: "sample", Limit: 1}
	page, err := c.Search(ctx, request)
	if err != nil || page.NextCursor == nil {
		t.Fatal("missing initial cursor", err)
	}
	saved, err := c.Account(ctx, online.AccountRequest{Action: "state"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Account(ctx, online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	if _, err = c.Account(ctx, online.AccountRequest{Action: "restore", Secret: saved.Secret}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3100 * time.Millisecond)
	checked, err := c.Account(ctx, online.AccountRequest{Action: "check"})
	if err != nil || checked.View.Revision == saved.View.Revision {
		t.Fatal("login generation reused", err)
	}
	request.Cursor = *page.NextCursor
	_, err = c.Search(ctx, request)
	expectCode(t, err, "INVALID_ARGUMENT")
}
