package playback_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestOnlineRangeSharesGrowingAudioAndReleasesTemporaryFiles(t *testing.T) {
	wave, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	for _, pref := range []struct{ retain, stream bool }{{true, true}, {false, true}, {true, false}} {
		retain := pref.retain
		t.Run(fmt.Sprintf("retain=%t,stream=%t", retain, pref.stream), func(t *testing.T) {
			f := newNetworkPlaybackFixture(t)
			release := sync.OnceFunc(func() { close(f.release) })
			defer release()
			f.provider.length.Store(int64(len(wave)))
			if !retain || !pref.stream {
				values := settings.Values{"streamWhileDownloading": pref.stream}
				if !retain {
					values["cacheLimitMiB"] = 0
				}
				_, err := f.store.PutPluginSettings(t.Context(), "admin", pluginproto.OnlinePluginID(online.Bilibili), 0, values)
				if err != nil {
					t.Fatal(err)
				}
			}
			d := f.next(t, "range-session")
			if d.SeekMode != "byteRange" || d.SeekableRange == nil || d.SeekableRange.EndMs != 6000 || !d.SeekableRange.Growing {
				t.Fatal(d)
			}
			head := f.request(t, http.MethodHead, d.StreamURL, "", nil)
			head.Body.Close()
			etag := head.Header.Get("ETag")
			if etag == "" || head.ContentLength != int64(len(wave)) || head.Header.Get("Accept-Ranges") != "bytes" || f.provider.opens.Load() != 0 {
				t.Fatal("invalid range probe", head.Header)
			}
			for _, probe := range []struct {
				headers map[string]string
				status  int
			}{
				{map[string]string{"Range": "bytes=0-7,16-31"}, 206},
				{map[string]string{"If-None-Match": etag}, 304},
				{map[string]string{"Range": "bytes=0-10", "If-Range": `"other"`}, 200},
			} {
				res := f.request(t, http.MethodHead, d.StreamURL, "", probe.headers)
				res.Body.Close()
				if res.StatusCode != probe.status || f.provider.opens.Load() != 0 {
					t.Fatal("unexpected HEAD with preconditions", res.Status)
				}
			}
			read := func(url string, start, end int, headers map[string]string) {
				t.Helper()
				headers["Range"] = fmt.Sprintf("bytes=%d-%d", start, end)
				res := f.request(t, http.MethodGet, url, "", headers)
				defer res.Body.Close()
				data, err := io.ReadAll(res.Body)
				if err != nil || res.StatusCode != 206 || res.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", start, end, len(wave)) || !bytes.Equal(data, wave[start:end+1]) {
					t.Fatalf("unexpected range %d-%d: status=%d, bytes=%d, err=%v", start, end, res.StatusCode, len(data), err)
				}
			}
			unchanged := f.request(t, http.MethodGet, d.StreamURL, "", map[string]string{"If-None-Match": etag})
			unchanged.Body.Close()
			if unchanged.StatusCode != 304 || f.provider.opens.Load() != 0 {
				t.Fatal("conditional request started download")
			}
			if !pref.stream {
				release()
			}
			read(d.StreamURL, 0, 31, map[string]string{"If-Range": etag})
			ctx, cancel := context.WithCancel(t.Context())
			sent := make(chan struct{})
			ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { close(sent) }})
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, f.api.URL+d.StreamURL, nil)
			req.AddCookie(&http.Cookie{Name: api.CookieName, Value: f.cookie})
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", len(wave)/2))
			waited := make(chan struct{})
			go func() {
				defer close(waited)
				if res, err := f.api.Client().Do(req); err == nil {
					defer res.Body.Close()
					io.Copy(io.Discard, res.Body)
				}
			}()
			select {
			case <-sent:
			case <-time.After(3 * time.Second):
				t.Fatal("range request was not sent")
			}
			read(d.StreamURL, 23, 29, map[string]string{})
			if pref.stream {
				select {
				case <-waited:
					t.Fatal("future range returned before its bytes arrived")
				default:
				}
			}
			cancel()
			select {
			case <-waited:
			case <-time.After(3 * time.Second):
				t.Fatal("canceled range still waiting")
			}
			release()
			read(d.StreamURL, len(wave)/2, len(wave)-1, map[string]string{})
			first := f.request(t, http.MethodGet, d.StreamURL, "", map[string]string{"Range": "bytes=0-"})
			whole, err := io.ReadAll(first.Body)
			first.Body.Close()
			if err != nil || first.StatusCode != 206 || first.Header.Get("Content-Range") != fmt.Sprintf("bytes 0-%d/%d", len(wave)-1, len(wave)) || !bytes.Equal(whole, wave) {
				t.Fatal("invalid browser range probe", first.Status, err)
			}
			if f.provider.opens.Load() != 1 {
				t.Fatal("range repeated upstream download", f.provider.opens.Load())
			}
			unsatisfied := f.request(t, http.MethodGet, d.StreamURL, "", map[string]string{"Range": "bytes=" + strconv.Itoa(len(wave)) + "-"})
			unsatisfied.Body.Close()
			if unsatisfied.StatusCode != 416 {
				t.Fatal("unexpected unsatisfied range status", unsatisfied.StatusCode)
			}
			stop := f.request(t, http.MethodDelete, "/api/v1/playback/sessions/"+d.SessionID, "", nil)
			stop.Body.Close()
			if retain {
				other := f.next(t, "cached-range")
				read(other.StreamURL, 18, 90, map[string]string{})
				if f.provider.opens.Load() != 1 {
					t.Fatal("cached seek reopened upstream")
				}
			} else {
				dir := filepath.Join(f.cacheRoot, "playback", "online")
				deadline := time.Now().Add(3 * time.Second)
				for {
					files, err := os.ReadDir(dir)
					if err != nil {
						t.Fatal(err)
					}
					if len(files) == 0 || len(files) == 1 && files[0].Name() == "last-cleanup" {
						break
					}
					if time.Now().After(deadline) {
						t.Fatal("session retained temporary audio", files)
					}
					time.Sleep(10 * time.Millisecond)
				}
				versions, err := f.store.ListOnlineDownloads(t.Context())
				if err != nil || len(versions) != 0 {
					t.Fatal("disabled retention published an audio file", versions, err)
				}
			}
		})
	}
}
