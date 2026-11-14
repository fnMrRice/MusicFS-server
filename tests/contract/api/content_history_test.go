package api_test

import (
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/cdalbum"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite/state"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestPlaybackReceiptRoutesOnlyToOwningHistory(t *testing.T) {
	f := newPackageCatalogHTTP(t)
	service := personal.New(f.store)
	path := filepath.Join(t.TempDir(), "plugins.sqlite")
	for _, plugin := range []struct {
		d contenttype.Descriptor
		h func(contenttype.HistoryState) contenttype.History
	}{{cdalbum.Descriptor(), cdalbum.History}, {asmr.Descriptor(), asmr.History}, {radio.Descriptor(), radio.History}} {
		db, err := state.Open(t.Context(), path, plugin.d.PluginID, "history")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Close() })
		service.RegisterHistory(plugin.d.ID, plugin.h(db))
	}
	f.server.RegisterPersonal(service)
	f.scan(t, "initial-history-scan")
	var scanned library.Page
	f.call(t, "GET", "/source-tracks", nil, "", 200, "SourceTracks", &scanned)
	sources := map[string]string{}
	for _, track := range scanned.Items {
		sources[*track.Title] = track.ID
	}
	for _, item := range []struct{ name, id string }{{"cd-only", "cd_album"}, {"asmr-only", "asmr"}, {"radio-only", "radio"}, {"music-only", "music"}} {
		p := f.importCopy(t, item.name, item.id, "directory")
		// 目录扫描的来源也可以按位置认领，即使导入副本使用另一个来源编号。
		id := sources[item.name]
		var session playback.Description
		f.call(t, "POST", "/playback/sessions", playback.CreateRequest{SourceTrackID: id,
			Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}},
			Preferences:  playback.Preferences{DSPExecution: "client"}}, "play-"+id, 201, "Playback", &session)
		body := personal.PlaybackStarted{SourceTrackID: id, PlaybackSessionID: session.SessionID, AttemptID: session.AttemptID}
		for range 2 {
			f.call(t, "POST", "/me/recently-played", body, "", 200, "PersonalCollectionEntry", nil)
		}
		if item.id != "music" {
			var history struct {
				Items []contenttype.HistoryEntry `json:"items"`
			}
			f.call(t, "GET", "/me/content-history/"+item.id, nil, "", 200, "ContentHistory", &history)
			if len(history.Items) != 1 || history.Items[0].WorkID != p.WorkID || history.Items[0].SourceTrackID != id {
				t.Fatalf("unexpected history for %s: %+v", item.id, history.Items)
			}
		}
		f.call(t, "DELETE", "/playback/sessions/"+session.SessionID, nil, "", 204, "", nil)
	}
	var ordinary personal.Page
	f.call(t, "GET", "/me/recently-played", nil, "", 200, "PersonalCollectionPage", &ordinary)
	if len(ordinary.Items) != 1 || ordinary.Items[0].Track.ID != sources["music-only"] {
		t.Fatalf("unexpected ordinary history: %+v", ordinary)
	}
	f.call(t, "GET", "/me/content-history/music", nil, "", 404, "Error", nil)
	f.cookie = f.otherUserCookie(t)
	var other struct {
		Items []contenttype.HistoryEntry `json:"items"`
	}
	f.call(t, "GET", "/me/content-history/asmr", nil, "", 200, "ContentHistory", &other)
	if len(other.Items) != 0 {
		t.Fatal("history leaked across accounts")
	}
}
