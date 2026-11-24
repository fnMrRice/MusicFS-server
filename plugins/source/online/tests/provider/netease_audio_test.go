package provideronline_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/netease"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

const neteaseMediaMD5 = "a5ca0b5894324f8bb54bb9fffad29d1e"

func audioJSON(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}
}

func TestNeteasePlaybackQualityThroughWorker(t *testing.T) {
	field := netease.PlaybackQualityField()
	if field.Key != "playbackQuality" || field.DefaultValue != "exhigh" || len(field.Options) != 7 {
		t.Fatalf("unexpected playback options: %+v", field)
	}
	for _, option := range field.Options {
		t.Run(option.Value, func(t *testing.T) {
			t.Parallel()
			peer := worker(t, transport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/song/enhance/player/url/v1" {
					q := r.URL.Query()
					if q.Get("ids") != "[42]" || q.Get("level") != option.Value || (q.Get("immerseType") == "aac") != (option.Value == "sky") || !strings.Contains(r.Header.Get("Cookie"), "MUSIC_U=") {
						t.Errorf("unexpected NetEase player request")
					}
					return audioJSON(fmt.Sprintf(`{"code":200,"data":[{"id":42,"code":200,"level":"standard","md5":"%s","type":"mp3","size":5,"br":128000,"sr":48000,"url":"http://m8.music.126.net/example/song?token=test"}]}`, neteaseMediaMD5)), nil
				}
				if r.URL.Host != "m8.music.126.net" || r.URL.Scheme != "https" || r.Header.Get("Cookie") != "" {
					t.Errorf("unexpected media request")
				}
				return audioJSON("audio"), nil
			}))
			hello(t, peer)
			request := pluginproto.OnlineAudioReference{Context: scope(), ExternalID: "42", Quality: option.Value}
			var metadata pluginproto.OnlineResult[pluginproto.OnlineAudio]
			assertCode(t, peer.Call(context.Background(), "provider.online.audioDescribe", request, &metadata), "PLATFORM_AUTH_REQUIRED")
			authenticate(t, peer)
			if err := peer.Call(context.Background(), "provider.online.audioDescribe", request, &metadata); err != nil {
				t.Fatal(err)
			}
			if metadata.Value.Quality != "standard" || metadata.Value.Version != neteaseMediaMD5 || metadata.Value.MIME != "audio/mpeg" || metadata.Value.SampleRateHz != 48000 {
				t.Fatalf("wrong delivered rendition: %+v", metadata.Value)
			}
			var opened pluginproto.OnlineResult[pluginproto.OnlineAudioOpened]
			if err := peer.Call(context.Background(), "provider.online.audioOpen", request, &opened); err != nil {
				t.Fatal(err)
			}
			if opened.Value.Audio.Quality != "standard" || opened.Value.Audio.Version != neteaseMediaMD5 || opened.Value.Audio.SampleRateHz != 48000 {
				t.Fatalf("wrong streamed rendition: %+v", opened.Value)
			}
			var chunk pluginproto.OnlineResult[pluginproto.OnlineAudioChunk]
			if err := peer.Call(context.Background(), "provider.online.audioRead", pluginproto.OnlineAudioRead{Context: scope(), StreamID: opened.Value.StreamID}, &chunk); err != nil || string(chunk.Value.Data) != "audio" {
				t.Fatalf("received %q, %v", chunk.Value.Data, err)
			}
		})
	}
}
