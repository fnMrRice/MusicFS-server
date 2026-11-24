package online_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	online "github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/adapter"
)

func TestNeteaseLogoutStopsOpenMedia(t *testing.T) {
	mediaStarted := make(chan context.Context, 1)
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/song/enhance/player/url/v1" {
			if r.URL.Query().Get("level") != "exhigh" {
				t.Error("expected default playback quality")
			}
			return response(`{"code":200,"data":[{"id":42,"code":200,"level":"lossless","size":4,"type":"flac","url":"https://m8.music.126.net/audio"}]}`).RoundTrip(r)
		}
		mediaStarted <- r.Context()
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("flac")), ContentLength: 4}, nil
	}))
	description, body, err := c.OpenAudio(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if description.MIME != "audio/flac" {
		t.Fatal("unexpected media type")
	}
	if _, err := c.Account(context.Background(), online.AccountRequest{Action: "logout"}); err != nil {
		t.Fatal(err)
	}
	media := <-mediaStarted
	select {
	case <-media.Done():
	case <-time.After(time.Second):
		t.Fatal("CDN request remained active after logout")
	}
	if _, err := body.Read(make([]byte, 4)); !errors.Is(err, context.Canceled) {
		t.Fatalf("revoked response returned %v", err)
	}
}

func TestNeteasePlaybackChannelLayout(t *testing.T) {
	for _, tc := range []struct {
		layout   string
		channels int
	}{{"null", 0}, {`"mono"`, 1}, {`"stereo"`, 2}, {`"5.1"`, 6}, {`"unknown"`, 0}} {
		t.Run(tc.layout, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				return response(`{"code":200,"data":[{"id":42,"code":200,"level":"lossless","size":400,"type":"flac","url":"https://m8.music.126.net/audio","channelLayout":` + tc.layout + `}]}`).RoundTrip(r)
			}))
			audio, err := c.DescribeAudio(t.Context(), "42")
			if err != nil || audio.Channels != tc.channels {
				t.Fatalf("channels=%d error=%v", audio.Channels, err)
			}
		})
	}
}

func TestNeteaseRejectsUnavailableOrIncompleteMedia(t *testing.T) {
	for _, tc := range []struct{ name, item, code string }{
		{"preview", `"url":"https://m8.music.126.net/song","freeTrialInfo":{"start":0,"end":30}`, "ACCESS_DENIED"},
		{"missing", `"url":null`, "ACCESS_DENIED"},
		{"destination", `"url":"https://unrelated.invalid/song"`, "ACCESS_DENIED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/api/song/enhance/player/url/v1" {
					t.Fatal("unexpected media request")
				}
				return response(`{"code":200,"data":[{"id":42,"code":200,"level":"exhigh","type":"mp3","size":128,` + tc.item + `}]}`).RoundTrip(r)
			}))
			_, _, err := c.OpenAudio(context.Background(), "42")
			expectCode(t, err, tc.code)
		})
	}
}

func TestNeteaseChecksDownloadedBytesAgainstVersion(t *testing.T) {
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/song/enhance/player/url/v1" {
			return response(`{"code":200,"data":[{"id":42,"code":200,"level":"sky","size":5,"md5":"a5ca0b5894324f8bb54bb9fffad29d1e","type":"m4a","encodeType":"aac","url":"https://m8.music.126.net/song"}]}`).RoundTrip(r)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: 5, Body: io.NopCloser(strings.NewReader("wrong"))}, nil
	}))
	audio, body, err := c.OpenAudio(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	if audio.MIME != "audio/mp4" || audio.Codec != "aac" {
		t.Fatal("unexpected AAC format", audio)
	}
	_, err = io.ReadAll(body)
	expectCode(t, err, "RESOURCE_CHANGED")
}
