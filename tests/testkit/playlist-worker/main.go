// This synthetic playlist worker is built only by tests, never by release scripts.
package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type fixture struct{ observations atomic.Int32 }

func (f *fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasPrefix(r.URL.Path, "/eapi/") && !strings.Contains(r.URL.Path, "/login/") && r.URL.Path != "/eapi/nuser/account/get" {
		var err error
		r, err = testkit.DecodeNeteaseFixture(r)
		if err != nil {
			return nil, err
		}
	}
	switch r.URL.Path {
	case "/api/v3/song/detail":
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"songs":[{"id":42,"name":"First"},{"id":43,"name":"Second"}]}`))}, nil
	case "/eapi/login/qrcode/unikey":
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"unikey":"playlist-fixture-challenge"}`))}, nil
	case "/eapi/login/qrcode/client/login":
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": []string{"MUSIC_U=playlist-fixture-token; Secure"}}, Body: io.NopCloser(strings.NewReader(`{"code":803}`))}, nil
	case "/eapi/nuser/account/get":
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"profile":{"userId":12345,"nickname":"Playlist listener"}}`))}, nil
	}
	if r.URL.Path != "/api/v6/playlist/detail" {
		return &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unexpected endpoint"))}, nil
	}
	if r.URL.Query().Get("id") == "92" {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	if r.URL.Query().Get("id") != "91" {
		panic("unexpected fixture identity")
	}
	status, body := 200, `{"code":200,"playlist":{"id":91,"name":"Synthetic","privacy":0,"trackCount":3,"trackIds":[{"id":42},{"id":42},{"id":43}]}}`
	switch n := f.observations.Add(1); {
	case n == 2:
		status, body = 500, "private upstream fixture detail"
	case n >= 3:
		body = `{"code":200,"playlist":{"id":91,"name":"Synthetic","privacy":0,"trackCount":2,"trackIds":[{"id":42},{"id":43}]}}`
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func main() {
	_ = onlineplugins.Serve(context.Background(), online.Netease, os.Stdin, os.Stdout, &fixture{})
}
