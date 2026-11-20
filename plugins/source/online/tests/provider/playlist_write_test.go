package provideronline_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestOwnedPrivatePlaylistWriteUsesCurrentAccountAndRetriesSafely(t *testing.T) {
	t.Parallel()
	var writes atomic.Int32
	peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
		body := ""
		switch r.URL.Path {
		case "/api/v6/playlist/detail":
			id := r.URL.Query().Get("id")
			owner := "12345"
			if id == "93" {
				owner = "67890"
			}
			members, count := "[]", 0
			if writes.Load() > 0 {
				members, count = `[{"id":42}]`, 1
			}
			body = fmt.Sprintf(`{"code":200,"playlist":{"id":%s,"userId":%s,"name":"Private","privacy":10,"trackCount":%d,"trackIds":%s}}`, id, owner, count, members)
		case "/api/playlist/manipulate/tracks":
			q := r.URL.Query()
			if q.Get("pid") != "91" || q.Get("trackIds") != `["42"]` || q.Get("op") != "add" || q.Get("imme") != "true" {
				t.Errorf("unexpected playlist mutation %v", q)
			}
			writes.Add(1)
			body = `{"code":200}`
		default:
			return nil, fmt.Errorf("unexpected request %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	}))
	hello(t, peer)
	ctx := context.Background()
	req := pluginproto.OnlinePlaylistTrackAddition{Context: scope(), PlaylistTrackAddition: online.PlaylistTrackAddition{AccountID: "12345", ExternalID: "91", TrackID: "42"}}
	const method = "provider.online.addPlaylistTrack"
	assertCode(t, peer.Call(ctx, method, req, nil), "PLATFORM_AUTH_REQUIRED")
	authenticate(t, peer)
	req.AccountID = "67890"
	assertCode(t, peer.Call(ctx, method, req, nil), "ACCESS_DENIED")
	req.AccountID = "12345"
	for i := 0; i < 2; i++ {
		var result pluginproto.OnlineResult[bool]
		if err := peer.Call(ctx, method, req, &result); err != nil || !result.Value || result.Context != req.Context {
			t.Fatalf("playlist addition %d failed: %v %+v", i, err, result)
		}
	}
	req.ExternalID = "93"
	assertCode(t, peer.Call(ctx, method, req, nil), "ACCESS_DENIED")
	if writes.Load() != 1 {
		t.Fatalf("expected one upstream mutation, got %d", writes.Load())
	}
}
