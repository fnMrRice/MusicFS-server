package provideronline_test

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	provideronline "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestTrackDetailsCapabilityOnlyAdvertisesImplementedProviders(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		descriptor := provideronline.Describe(platform)
		found := false
		for _, capability := range descriptor.Capabilities {
			if capability.Name == "track.details" {
				found = true
				if capability.Supported != (platform == online.Netease) {
					t.Fatalf("unexpected details capability for %s", platform)
				}
			}
		}
		if !found {
			t.Fatalf("details capability is missing for %s", platform)
		}
	}
}

func TestOnlineWorkerReturnsProviderTrackDetailsWithBoundScope(t *testing.T) {
	var calls atomic.Int32
	peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/api/v3/song/detail" {
			t.Errorf("unexpected upstream endpoint: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"songs":[{"id":42,"name":"Provider detail title","ar":[{"name":"Artist"}],"al":{"name":"Album"},"dt":123000}]}`))}, nil
	}))
	hello(t, peer)
	request := pluginproto.OnlineReference{Context: scope(), ExternalID: "42"}
	assertCode(t, peer.Call(context.Background(), "provider.online.trackDetails", request, nil), "PLATFORM_AUTH_REQUIRED")
	authenticate(t, peer)
	other := request
	other.Context.UserID = "other"
	assertCode(t, peer.Call(context.Background(), "provider.online.trackDetails", other, nil), "ACCESS_DENIED")
	var result pluginproto.OnlineResult[pluginproto.TrackDetails]
	if err := peer.Call(context.Background(), "provider.online.trackDetails", request, &result); err != nil {
		t.Fatal(err)
	}
	if result.Context != request.Context || result.Value.Title != "Provider detail title" || result.Value.Validate("42") != nil || calls.Load() != 1 {
		t.Fatalf("invalid scoped details response: %+v", result)
	}
}
