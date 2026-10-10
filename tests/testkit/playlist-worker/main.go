// This synthetic playlist worker is built only by tests, never by release scripts.
package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/provideronline"
)

type fixture struct{ observations atomic.Int32 }

func (f *fixture) RoundTrip(r *http.Request) (*http.Response, error) {
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
	_ = provideronline.Serve(context.Background(), online.Netease, os.Stdin, os.Stdout, &fixture{})
}
