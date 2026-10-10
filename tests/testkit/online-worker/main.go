// This synthetic worker is compiled only by tests, never by release scripts.
package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/provideronline"
)

type fixture struct{}

func (fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Query().Get("s") == "slow" {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	status, body := 200, `{"code":200,"result":{"songs":[{"id":42,"name":"Synthetic","artists":[{"name":"Artist"}],"album":{"name":"Album"},"duration":120000}],"songCount":1,"hasMore":false}}`
	switch r.URL.Path {
	case "/eapi/login/qrcode/unikey":
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"unikey":"fixture-challenge"}`))}, nil
	case "/eapi/login/qrcode/client/login":
		return &http.Response{StatusCode: 200, Header: http.Header{"Set-Cookie": []string{"MUSIC_U=fixture-account-token; Secure"}}, Body: io.NopCloser(strings.NewReader(`{"code":803}`))}, nil
	case "/eapi/nuser/account/get":
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":200,"profile":{"userId":12345,"nickname":"Fixture listener"}}`))}, nil
	case "/api/v3/song/detail":
		body = `{"code":200,"songs":[{"id":42,"name":"Synthetic","ar":[],"al":{"name":"Album"},"dt":120000}]}`
	case "/api/personalized/playlist":
		body = `{"code":200,"result":[{"id":91,"name":"Synthetic","copywriter":"Public","trackCount":3}]}`
	case "/api/v6/playlist/detail":
		body = `{"code":200,"playlist":{"id":91,"name":"Synthetic","privacy":0,"trackCount":3,"trackIds":[{"id":42},{"id":42},{"id":43}]}}`
	case "/soso/fcgi-bin/client_search_cp":
		status, body = 500, ""
	}
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func main() {
	name := filepath.Base(os.Args[0])
	if strings.Contains(name, "bad-") {
		badWorker(name)
		return
	}
	platform := online.Netease
	if strings.Contains(name, "qqfixture") {
		platform = online.QQMusic
	}
	_ = provideronline.Serve(context.Background(), platform, os.Stdin, os.Stdout, fixture{})
}

func badWorker(mode string) {
	var hello pluginproto.OnlineHello
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "host.hello" {
			if err := pluginproto.DecodeOnline(raw, &hello); err != nil {
				return nil, err
			}
			return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.netease", PluginVersion: "0.1.0", ProtocolVersion: "1.0", Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(online.Netease), Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}}, nil
		}
		if method == "provider.online.describe" {
			var r pluginproto.OnlineScope
			_ = json.Unmarshal(raw, &r)
			return pluginproto.OnlineResult[pluginproto.OnlineDescriptor]{Context: r.Context, Value: pluginproto.DescribeOnline(online.Netease)}, nil
		}
		var r pluginproto.OnlineSearch
		_ = json.Unmarshal(raw, &r)
		if strings.Contains(mode, "scope") {
			r.Context.UserID = "another-owner"
		}
		client, _ := online.New(online.Netease, online.Scope{UserID: "test", InstanceID: "test"}, online.Options{Transport: fixture{}})
		defer client.Close()
		page, err := client.Search(ctx, online.SearchRequest{Query: "fixture", Limit: 1})
		if err != nil {
			return nil, err
		}
		if strings.Contains(mode, "url") {
			page.Tracks[0].SourceURL = "http://127.0.0.1/private"
		}
		return pluginproto.OnlineResult[online.SearchPage]{Context: r.Context, Value: page}, nil
	})
	<-peer.Done()
}
