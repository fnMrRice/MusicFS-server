package provideronline_test

import (
	"context"
	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestOnlineWorkerListsOnlyItsAuthenticatedAccountPlaylists(t *testing.T) {
	peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/user/playlist" || r.URL.Query().Get("uid") != "12345" {
			t.Fatal("unexpected account playlist request", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"more":false,"playlist":[{"id":91,"name":"Account playlist","trackCount":2}]}`))}, nil
	}))
	hello(t, peer)
	request := pluginproto.OnlinePlaylists{Context: scope(), PlaylistsRequest: online.PlaylistsRequest{Limit: 20}}
	assertCode(t, peer.Call(context.Background(), "provider.online.playlists", request, nil), "PLATFORM_AUTH_REQUIRED")
	authenticate(t, peer)
	var result pluginproto.OnlineResult[online.PlaylistsPage]
	if err := peer.Call(context.Background(), "provider.online.playlists", request, &result); err != nil || len(result.Value.Items) != 1 || result.Value.Items[0].ExternalID != "91" || online.ValidatePlaylists(onlineplugins.Describe(online.Netease).Resources, result.Value, 20) != nil {
		t.Fatal("invalid account playlist RPC result", result, err)
	}
	assertCode(t, peer.Call(context.Background(), "provider.online.playlists", map[string]any{"context": scope(), "limit": 20, "cursor": "", "userId": "other"}, nil), "PROTOCOL_MISMATCH")
}
