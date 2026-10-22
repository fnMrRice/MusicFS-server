package online_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func TestTrackDetailsUseProviderFieldsWithoutPrivateResources(t *testing.T) {
	var calls atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		if r.URL.Path != "/api/v3/song/detail" || r.URL.Query().Get("c") != `[{"id":42}]` {
			t.Fatalf("unexpected detail request: %s", r.URL)
		}
		return response(`{"code":200,"songs":[{"id":42,"name":"曲目","ar":[{"name":"甲"},{"name":"乙"}],"al":{"name":"专辑"},"dt":120001,"alia":["别称"],"tns":["Translation"],"mp3Url":"https://private.invalid/token=secret","h":{"br":999999}}],"token":"fixture-private-token"}`).RoundTrip(r)
	}))
	details, err := c.TrackDetails(context.Background(), "42")
	if err != nil || details.Validate("42") != nil {
		t.Fatalf("invalid details: %+v, %v", details, err)
	}
	expected := []pluginproto.TrackDetailField{
		{ID: "title", Label: "歌曲名", Value: "曲目"},
		{ID: "artists", Label: "作者 / 艺人", Value: "甲 / 乙"},
		{ID: "album", Label: "专辑", Value: "专辑"},
		{ID: "duration", Label: "时长", Value: "2:00.001"},
		{ID: "aliases", Label: "别名", Value: "别称"},
		{ID: "translations", Label: "译名", Value: "Translation"},
	}
	if details.Title != "曲目" || len(details.Sections) != 2 || details.Sections[0].ID != "track" || !reflect.DeepEqual(details.Sections[0].Fields, expected) || calls.Load() != 1 {
		t.Fatalf("unexpected provider details: %+v", details)
	}
	encoded, _ := json.Marshal(details)
	for _, private := range []string{"fixture-private-token", "private.invalid", "999999", "metadataDigest"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("unexpected private or inferred field: %s", private)
		}
	}
	if details.Sections[1].Fields[2].Value != "https://music.163.com/song?id=42" {
		t.Fatal("public track page is missing")
	}
}

func TestTrackDetailsOmitUnknownFieldsAndRejectInvalidMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, body, code string
	}{
		{"minimal", `{"code":200,"songs":[{"id":42,"name":"Track","ar":[],"al":{},"dt":0}]}`, ""},
		{"different identity", `{"code":200,"songs":[{"id":43,"name":"Track","ar":[],"al":{}}]}`, "TEMPORARY_FAILURE"},
		{"missing", `{"code":200,"songs":[]}`, "NOT_FOUND"},
		{"invalid alias", `{"code":200,"songs":[{"id":42,"name":"Track","ar":[],"al":{},"alia":["bad\u0000value"]}]}`, "TEMPORARY_FAILURE"},
		{"oversized aliases", `{"code":200,"songs":[{"id":42,"name":"Track","ar":[],"al":{},"alia":["` + strings.Repeat("x", 4097) + `"]}]}`, "TEMPORARY_FAILURE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := scoped(t, online.Netease, response(tc.body))
			details, err := c.TrackDetails(context.Background(), "42")
			if tc.code != "" {
				expectCode(t, err, tc.code)
				return
			}
			if err != nil || details.Validate("42") != nil || len(details.Sections[0].Fields) != 1 {
				t.Fatalf("unknown metadata was fabricated: %+v, %v", details, err)
			}
		})
	}
}

func TestTrackDetailsRespectAuthorizationCapabilityAndCancellation(t *testing.T) {
	var calls atomic.Int32
	transport := roundTrip(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected upstream request")
	})
	c := anonymousScoped(t, online.Netease, transport)
	_, err := c.TrackDetails(context.Background(), "42")
	expectCode(t, err, "PLATFORM_AUTH_REQUIRED")
	c = scoped(t, online.QQMusic, transport)
	_, err = c.TrackDetails(context.Background(), "42")
	expectCode(t, err, "UNSUPPORTED")
	c = scoped(t, online.Netease, transport)
	_, err = c.TrackDetails(context.Background(), "https://127.0.0.1/private")
	expectCode(t, err, "INVALID_ARGUMENT")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.TrackDetails(ctx, "42")
	if !errors.Is(err, context.Canceled) || calls.Load() != 0 {
		t.Fatalf("invalid request reached upstream: calls=%d, error=%v", calls.Load(), err)
	}
}
