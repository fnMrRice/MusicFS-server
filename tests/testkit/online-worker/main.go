// This synthetic worker is compiled only by tests, never by release scripts.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type fixture struct {
	accountRead         atomic.Bool
	recommendationReads atomic.Uint32
}

func (f *fixture) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasPrefix(r.URL.Path, "/eapi/") && !strings.Contains(r.URL.Path, "/login/") && r.URL.Path != "/eapi/nuser/account/get" {
		var err error
		r, err = testkit.DecodeNeteaseFixture(r)
		if err != nil {
			return nil, err
		}
	}

	if len(os.Args) == 2 && os.Args[1] == "playlist-download" {
		if res := downloadFixture(r); res != nil {
			return res, nil
		}
	}
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
		body = `{"code":200,"profile":{"userId":12345,"vipType":11,"nickname":"Fixture listener","avatarUrl":"http://p2.music.126.net/fixture/avatar.jpg"}}`
		if len(os.Args) == 2 && !f.accountRead.Swap(true) {
			switch os.Args[1] {
			case "account-offline":
				status = http.StatusServiceUnavailable
			case "account-expired":
				status = http.StatusUnauthorized
			}
		}
	case "/api/search/get":
		switch r.URL.Query().Get("type") {
		case "10":
			body = `{"code":200,"result":{"albums":[{"id":91,"name":"Album","size":2}],"albumCount":1}}`
		case "100":
			body = `{"code":200,"result":{"artists":[{"id":92,"name":"Artist","albumSize":1,"musicSize":2}],"artistCount":1}}`
		case "1000":
			body = `{"code":200,"result":{"playlists":[{"id":91,"name":"Playlist","trackCount":3}],"playlistCount":1}}`
		}
	case "/api/v1/album/91":
		body = `{"code":200,"album":{"id":91,"name":"Album","size":1},"songs":[{"id":42,"name":"Track","ar":[],"al":{},"dt":123}]}`
	case "/api/artist/92":
		body = `{"code":200,"artist":{"id":92,"name":"Artist","albumSize":1,"musicSize":2}}`
	case "/api/artist/albums/92":
		body = `{"code":200,"artist":{"id":92,"name":"Artist"},"hotAlbums":[{"id":91,"name":"Album","size":1}],"more":false}`
	case "/api/v1/artist/songs":
		body = `{"code":200,"songs":[{"id":42,"name":"Track","artists":[],"album":{},"duration":123}],"more":false,"total":1}`
	case "/api/v3/song/detail":
		body = `{"code":200,"songs":[{"id":42,"name":"Synthetic","ar":[],"al":{"name":"Album"},"dt":120000}]}`
		if len(os.Args) == 2 && os.Args[1] == "catalog-match" {
			body = catalogMatchSongs(r.URL.Query().Get("c"))
		}
	case "/api/song/enhance/player/url/v1":
		body = `{"code":200,"data":[{"id":42,"code":200,"type":"mp3","level":"exhigh","size":128,"url":"https://m8.music.126.net/fixture.mp3"}]}`
	case "/api/personalized/playlist":
		if len(os.Args) == 3 && os.Args[1] == "recommendation-rate-limit" && f.recommendationReads.Load() == 2 {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {os.Args[2]}}, Body: http.NoBody}, nil
		}
		body = `{"code":200,"result":[{"id":91,"name":"Synthetic","copywriter":"Public","trackCount":3}]}`
	case "/api/v3/discovery/recommend/songs":
		if len(os.Args) == 3 && os.Args[1] == "recommendation-rate-limit" && f.recommendationReads.Add(1) == 2 {
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {os.Args[2]}}, Body: http.NoBody}, nil
		}
		body = `{"code":200,"data":{"dailySongs":[{"id":42,"name":"Daily song","ar":[],"al":{"name":"Album","picUrl":"https://p2.music.126.net/daily.jpg"},"dt":120000}]}}`
	case "/api/user/playlist":
		if r.URL.Query().Get("uid") != "12345" {
			return nil, fmt.Errorf("account playlist request used an unexpected account")
		}
		body = `{"code":200,"more":false,"playlist":[{"id":91,"name":"Account playlist","trackCount":3}]}`
	case "/api/v6/playlist/detail":
		body = `{"code":200,"playlist":{"id":91,"name":"Synthetic","privacy":0,"trackCount":3,"trackIds":[{"id":42},{"id":42},{"id":43}]}}`
		if len(os.Args) == 2 && os.Args[1] == "catalog-match" {
			body = `{"code":200,"playlist":{"id":91,"name":"Favorites","userId":12345,"privacy":0,"trackCount":2,"trackIds":[{"id":42},{"id":43}]}}`
		}
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
	_ = onlineplugins.Serve(context.Background(), platform, os.Stdin, os.Stdout, &fixture{})
}

func badWorker(mode string) {
	var hello pluginproto.OnlineHello
	peer := pluginproto.NewPeer(context.Background(), os.Stdin, os.Stdout, "w-", func(ctx context.Context, method string, raw json.RawMessage) (any, error) {
		if method == "host.hello" {
			if err := pluginproto.DecodeOnline(raw, &hello); err != nil {
				return nil, err
			}
			return pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.plugin.source.netease", Plugin: onlineplugins.Describe(online.Netease).Plugin, SupportsMultipleInstances: true, PluginVersion: "0.1.0", ProtocolVersion: "1.0", SourceType: "network", Roles: []string{"provider"}, Capabilities: pluginproto.OnlineCapabilities(onlineplugins.Describe(online.Netease)), Systems: []string{runtime.GOOS}, Architectures: []string{runtime.GOARCH}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: online.MaxPageItems}}, nil
		}
		if method == "provider.online.describe" {
			var r pluginproto.OnlineScope
			_ = json.Unmarshal(raw, &r)
			return pluginproto.OnlineResult[pluginproto.OnlineDescriptor]{Context: r.Context, Value: onlineplugins.Describe(online.Netease)}, nil
		}
		if method == "provider.online.account" {
			var r pluginproto.OnlineAccount
			if err := json.Unmarshal(raw, &r); err != nil {
				return nil, err
			}
			return pluginproto.OnlineResult[online.AccountResult]{Context: r.Context, Value: online.AccountResult{View: online.AccountView{State: "signedIn", AccountID: "fixture-account", Revision: "fixture-revision"}}}, nil
		}
		var r pluginproto.OnlineSearch
		_ = json.Unmarshal(raw, &r)
		if strings.Contains(mode, "scope") {
			r.Context.UserID = "another-owner"
		}

		track := online.Track{ExternalID: "42", Title: "Fixture", Artists: []string{}, SourceURL: "https://music.163.com/song?id=42", PlaybackAvailability: "unknown"}
		encoded, _ := json.Marshal(track)
		sum := sha256.Sum256(encoded)
		track.MetadataDigest = fmt.Sprintf("%x", sum)
		page := online.SearchPage{Kind: "tracks", Tracks: []online.Track{track}, Albums: []online.Album{}, Artists: []online.Artist{}, Playlists: []online.Playlist{}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}

		if strings.Contains(mode, "url") {
			page.Tracks[0].SourceURL = "http://127.0.0.1/private"
		}
		return pluginproto.OnlineResult[online.SearchPage]{Context: r.Context, Value: page}, nil
	})
	<-peer.Done()
}
