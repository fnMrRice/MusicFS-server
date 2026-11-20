package api_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	onlineplugins "github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type neteaseLinkTransport struct{ writes chan string }

type neteaseLinkProvider struct{ *onlineplugins.Client }

func (neteaseLinkProvider) Descriptor() pluginproto.OnlineDescriptor {
	return onlineplugins.Describe(online.Netease)
}

func (p neteaseLinkTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := ""
	switch r.URL.Path {
	case "/api/v6/playlist/detail":
		body = `{"code":200,"playlist":{"id":91,"userId":12345,"name":"Private","privacy":10,"trackCount":0,"trackIds":[]}}`
	case "/api/playlist/manipulate/tracks":
		p.writes <- fmt.Sprintf("%s/%s/%s", r.URL.Query().Get("op"), r.URL.Query().Get("pid"), r.URL.Query().Get("trackIds"))
		body = `{"code":200}`
	default:
		return onlineTransport{}.RoundTrip(r)
	}
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
}

func TestAddingAccountTrackToLinkedPlaylist(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	const instance = "7fe223d0-a3bd-5e87-87f8-305dc26d9041"
	upstream := neteaseLinkTransport{make(chan string, 3)}
	client, err := onlineplugins.New(online.Netease, online.Scope{UserID: "admin", InstanceID: instance}, onlineplugins.Options{Transport: testkit.OnlineAccountTransport(upstream)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	testkit.RestoreOnlineFixture(t, client, online.Netease, "admin", instance)
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: instance, Platform: online.Netease, ConfigRevision: "r1", Enabled: true, Provider: neteaseLinkProvider{client}}})
	if err != nil {
		t.Fatal(err)
	}
	links := playlists.NewLinks(store, host)
	server.RegisterPlaylistTrackSources(host, store)
	server.RegisterPlaylistLinks(links)
	server.RegisterPlaylists(playlists.NewAggregate(store))
	request := func(method, path string, value any, status int, schema string) []byte {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "test-linked-playlist")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
		return w.Body.Bytes()
	}

	playlist, err := store.CreatePlaylist(t.Context(), "admin", playlists.CreatePlaylistRequest{Name: "Local", Bindings: []playlists.PlaylistBinding{}}, "create-empty-playlist")
	if err != nil {
		t.Fatal(err)
	}
	base := "/online-instances/" + instance
	request("PUT", base+"/playlists/91/link", map[string]string{"accountId": "12345", "playlistId": playlist.ID}, 200, "OnlinePlaylistLink")
	const sources = "/playlist-track-sources"
	request("POST", base+sources, map[string]any{"accountId": "other", "externalIds": []string{"42"}}, 403, "Error")
	request("POST", base+sources, map[string]any{"accountId": "12345", "externalIds": []string{}}, 400, "Error")
	// 插件对单实例的元数据请求执行每秒一次的准入限制。
	time.Sleep(1010 * time.Millisecond)
	raw := request("POST", base+sources, map[string]any{"accountId": "12345", "externalIds": []string{"42", "42"}}, 200, "OnlinePlaylistTrackSources")
	var result struct {
		Items []struct{ RemoteSourceID string }
	}
	if err := json.Unmarshal(raw, &result); err != nil || len(result.Items) != 2 || result.Items[0] != result.Items[1] {
		t.Fatalf("unexpected track references: %s", raw)
	}
	binding := playlists.PlaylistBinding{Kind: "local", Entries: []playlists.LocalEntry{{Source: playlists.SourceRef{RemoteSourceID: &result.Items[0].RemoteSourceID}}}}
	request("PUT", "/playlists/"+playlist.ID, playlists.EditPlaylistRequest{ExpectedRevision: playlist.RuleRevision, Name: playlist.Name, Bindings: []playlists.PlaylistBinding{binding}}, 200, "Playlist")
	raw = request("GET", "/playlist-links", nil, 200, "OnlinePlaylistLinks")
	var queued struct{ Items []playlists.PlaylistLink }
	if err := json.Unmarshal(raw, &queued); err != nil || len(queued.Items) != 1 || queued.Items[0].PendingCount != 1 {
		t.Fatalf("unexpected pending additions: %s", raw)
	}
	stop := links.Start(t.Context())
	defer stop()
	select {
	case write := <-upstream.writes:
		if write != `add/91/["42"]` {
			t.Fatalf("unexpected upstream mutation: %s", write)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("linked track did not reach the upstream playlist")
	}
}
