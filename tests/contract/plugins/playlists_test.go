package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAccountPlaylistsWireContract(t *testing.T) {
	scope := pluginproto.Context{InstanceID: "1463118e-e2d1-564a-ab70-05a357c1b9bf", UserID: "owner", TaskID: strings.Repeat("a", 48), TimeoutMs: 10000}
	request := pluginproto.OnlinePlaylists{Context: scope, PlaylistsRequest: online.PlaylistsRequest{Limit: 20}}
	onlineContract(t, "Playlists", request)
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": "provider.online.playlists", "params": request})
	page := online.PlaylistsPage{Items: []online.Playlist{{ExternalID: "91", Title: "Account playlist", SourceURL: "https://music.163.com/playlist?id=91", Writable: true}}, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}
	onlineContract(t, "PlaylistsPage", page)
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-2", "result": pluginproto.OnlineResult[online.PlaylistsPage]{Context: scope, Value: page}})
	addition := pluginproto.OnlinePlaylistTrackAddition{Context: scope, PlaylistTrackAddition: online.PlaylistTrackAddition{AccountID: "12345", ExternalID: "91", TrackID: "42"}}
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-3", "method": "provider.online.addPlaylistTrack", "params": addition})
	onlineContract(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-3", "result": pluginproto.OnlineResult[bool]{Context: scope, Value: true}})
	encoded, _ := json.Marshal(request)
	value := testkit.DecodeJSON(t, encoded).(map[string]any)
	value["userId"] = "another-account"
	if onlineSchema(t, "Playlists").Validate(value) == nil {
		t.Fatal("caller-selected playlist account accepted by schema")
	}
}
