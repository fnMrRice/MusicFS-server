package bilibili

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type watchedBody struct {
	reads  int
	closed bool
	data   *strings.Reader
}

func (b *watchedBody) Read(p []byte) (int, error) { b.reads++; return b.data.Read(p) }
func (b *watchedBody) Close() error               { b.closed = true; return nil }

func playFixture(raw string) map[string]any {
	return map[string]any{"timelength": 60000, "dash": map[string]any{"audio": []any{map[string]any{"baseUrl": raw, "mimeType": "audio/mp4", "codecs": "mp4a.40.2", "bandwidth": 128000}}}}
}

func TestOpenAudioDoesNotReadBodyBeforePlayback(t *testing.T) {
	body := &watchedBody{data: strings.NewReader("audio payload")}
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/x/web-interface/view":
			return response(t, videoFixture()), nil
		case "/x/player/playurl":
			if r.URL.Query().Get("cid") != "101" {
				t.Fatal("wrong part requested")
			}
			return response(t, playFixture("https://upos-sz-mirrorcos.bilivideo.com/audio.m4s?secret=temporary")), nil
		default:
			if r.Method != "GET" || r.Header.Get("Range") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Referer") != "https://www.bilibili.com/" {
				t.Fatal("unsafe audio request headers")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: -1, Body: body}, nil
		}
	})
	audio, stream, err := c.OpenAudio(context.Background(), "video:BV13x41117TL:101")
	if err != nil {
		t.Fatal(err)
	}
	if body.reads != 0 || body.closed || audio.SizeBytes != -1 || audio.MIME != "audio/mp4" || audio.Codec != "aac" {
		t.Fatal("audio was read or guessed to have stable bytes")
	}
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	stream.Close()
	if string(data) != "audio payload" || !body.closed {
		t.Fatal("stream body was not passed through")
	}
}

func TestDescribeAudioNeverContactsCDN(t *testing.T) {
	calls := 0
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Host != "api.bilibili.com" {
			t.Fatal("audio description contacted CDN")
		}
		if r.URL.Path == "/x/web-interface/view" {
			return response(t, videoFixture()), nil
		}
		return response(t, playFixture("https://upos.bilivideo.com/audio.m4s")), nil
	})
	audio, err := c.DescribeAudio(context.Background(), "video:BV13x41117TL:101")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || audio.SizeBytes != -1 || audio.MIME != "audio/mp4" || audio.Codec != "aac" || audio.BitrateBps == nil {
		t.Fatal("audio description lost upstream metadata")
	}
	if err := c.Resolve(context.Background(), "video:BV13x41117TL:101"); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatal("resolve did not use the metadata-only path")
	}
}

func TestAudioRedirectRequiresAllowedHTTPSDestination(t *testing.T) {
	for _, target := range []string{"http://upos-sz-mirrorcos.bilivideo.com/file", "https://127.0.0.1/file", "https://evil.example/file", "https://upos.bilivideo.com.evil/file", "https://user@upos.bilivideo.com/file"} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
				calls++
				return &http.Response{StatusCode: 302, Header: http.Header{"Location": {target}}, Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
			})
			_, err := c.openCDN(context.Background(), "https://upos.bilivideo.com/file")
			errorCode(t, err, "ACCESS_DENIED")
			if calls != 1 {
				t.Fatal("unsafe redirect was fetched")
			}
		})
	}
}

func TestAudioPreviewMixedAndUnsafeStreamsAreRejected(t *testing.T) {
	for _, tc := range []struct {
		name string
		data map[string]any
		code string
	}{
		{"preview", map[string]any{"timelength": 30000, "is_preview": 1}, "ACCESS_DENIED"},
		{"mixed", map[string]any{"timelength": 60000, "durl": []any{map[string]any{"url": "https://upos.bilivideo.com/video.mp4"}}}, "UNSUPPORTED"},
		{"unsafe", playFixture("http://127.0.0.1/audio.m4s"), "UNSUPPORTED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/x/web-interface/view" {
					return response(t, videoFixture()), nil
				}
				if r.URL.Path != "/x/player/playurl" {
					t.Fatal("unexpected CDN request")
				}
				return response(t, tc.data), nil
			})
			_, _, err := c.OpenAudio(context.Background(), "video:BV13x41117TL:101")
			errorCode(t, err, tc.code)
		})
	}
}

func TestAudioOversizeAndPartialBodyAreClosed(t *testing.T) {
	for _, tc := range []struct {
		name    string
		size    int64
		headers http.Header
	}{{"oversize", pluginproto.MaxOnlineAudioBytes + 1, http.Header{}}, {"partial", 10, http.Header{"Content-Range": {"bytes 0-9/100"}}}, {"encoding", 10, http.Header{"Content-Encoding": {"gzip"}}}} {
		t.Run(tc.name, func(t *testing.T) {
			body := &watchedBody{data: strings.NewReader("audio")}
			c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/x/web-interface/view" {
					return response(t, videoFixture()), nil
				}
				if r.URL.Path == "/x/player/playurl" {
					return response(t, playFixture("https://upos.bilivideo.com/audio")), nil
				}
				return &http.Response{StatusCode: 200, Header: tc.headers, ContentLength: tc.size, Body: body}, nil
			})
			_, _, err := c.OpenAudio(context.Background(), "video:BV13x41117TL:101")
			errorCode(t, err, "UNSUPPORTED")
			if !body.closed || body.reads != 0 {
				t.Fatal("rejected audio body was read or left open")
			}
		})
	}
}

func TestAudioAreaUsesMetadataAndDoesNotExposeURL(t *testing.T) {
	body := &watchedBody{data: strings.NewReader("audio")}
	c := newTestClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/song/info") {
			return response(t, map[string]any{"id": 10, "title": "Audio", "duration": 60}), nil
		}
		if strings.HasSuffix(r.URL.Path, "/web/url") {
			return response(t, map[string]any{"type": 1, "size": 5, "cdns": []string{"https://upos.bilivideo.com/audio.m4a"}}), nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, ContentLength: 5, Body: body}, nil
	})
	audio, stream, err := c.OpenAudio(context.Background(), "audio:10")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if audio.ExternalID != "audio:10" || audio.SizeBytes != 5 || body.reads != 0 {
		t.Fatal("unexpected audio area stream")
	}
}
