package provideronline_test

import (
	"context"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestWorkerDailySongsUseAccountAndRetainMetadataInOrder(t *testing.T) {
	t.Parallel()
	const payload = `{"code":200,"data":{"dailySongs":[
		{"id":9007199254740993,"name":"第一首","ar":[{"name":"甲"},{"name":"乙"}],"al":{"name":"专辑"},"dt":180001},
		{"id":52,"name":"第二首","ar":[]},{"id":12,"name":"第三首","ar":[]}]}}`
	peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v3/discovery/recommend/songs" || r.URL.RawQuery != "" {
			t.Errorf("unexpected daily songs request: %s", r.URL)
		}
		if _, err := r.Cookie("MUSIC_U"); err != nil {
			t.Error("daily songs request omitted the platform session")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}, nil
	}))
	hello(t, peer)
	ctx := context.Background()
	request := pluginproto.OnlineRecommendations{Context: scope(), Limit: 2}
	assertCode(t, peer.Call(ctx, "provider.online.dailySongs", request, nil), "PLATFORM_AUTH_REQUIRED")
	authenticate(t, peer)
	for _, limit := range []int{0, online.MaxPageItems + 1} {
		invalid := pluginproto.OnlineRecommendations{Context: scope(), Limit: limit}
		assertCode(t, peer.Call(ctx, "provider.online.dailySongs", invalid, nil), "INVALID_ARGUMENT")
	}
	var result pluginproto.OnlineResult[online.DailySongsPage]
	if err := peer.Call(ctx, "provider.online.dailySongs", request, &result); err != nil {
		t.Fatal(err)
	}
	if err := online.ValidateDailySongs(onlineplugins.Describe(online.Netease).Resources, result.Value, request.Limit); err != nil {
		t.Fatal(err)
	}
	tracks := result.Value.Tracks
	if len(tracks) != 2 || tracks[0].ExternalID != "9007199254740993" || tracks[0].Title != "第一首" ||
		!slices.Equal(tracks[0].Artists, []string{"甲", "乙"}) || tracks[0].Album != "专辑" ||
		tracks[0].DurationMs == nil || *tracks[0].DurationMs != 180001 || tracks[1].ExternalID != "52" {
		t.Fatal("daily songs order, metadata or limit changed", tracks)
	}
}
