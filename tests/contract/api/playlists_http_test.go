package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPlaylistsHTTPProvenanceHiddenConfirmationPrivacyAndRevisions(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	tracks := map[string]library.Track{}
	for _, owner := range []string{"admin", "other"} {
		provider, names := "316664dc-7106-5acd-8df7-88c9d0d9baef", []string{"a", "b", "c"}
		if owner == "other" {
			provider, names = "843558e1-a158-56ac-834a-baffd12bb5c5", []string{"private"}
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "playlist-fixture"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		items := []library.Observation{}
		for _, name := range names {
			items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: provider, RelativePath: "secret-path/" + name + ".wav", DisplayName: name, ObservedVersion: "v1", SizeBytes: 100}, Artists: []string{}})
		}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, len(items), nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, track := range page.Items {
			tracks[track.DisplayName] = track
		}
	}
	server.RegisterPlaylists(playlists.NewAggregate(store))
	local := func(names ...string) playlists.PlaylistBinding {
		b := playlists.PlaylistBinding{Kind: "local", Entries: []playlists.LocalEntry{}}
		for _, name := range names {
			id := tracks[name].ID
			b.Entries = append(b.Entries, playlists.LocalEntry{Source: playlists.SourceRef{SourceTrackID: &id}})
		}
		return b
	}
	request := func(method, path string, input any, key string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		raw, _ := json.Marshal(input)
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		for _, secret := range []string{"secret-path", tracks["private"].ID, `"owner"`} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("playlist leaked private source")
			}
		}
		return w
	}
	expect := func(method, path string, input any, key string, status int, schema string) *httptest.ResponseRecorder {
		t.Helper()
		w := request(method, path, input, key, nil)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
		return w
	}
	decodePlaylist := func(w *httptest.ResponseRecorder) playlists.Playlist {
		t.Helper()
		var p playlists.Playlist
		if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	childInput := playlists.CreatePlaylistRequest{Name: "Child", Bindings: []playlists.PlaylistBinding{local("a", "b", "a")}}
	child := decodePlaylist(expect("POST", "/playlists", childInput, "create-child", 201, "Playlist"))
	if child.RuleRevision != 1 || child.HideRevision != 0 || child.SnapshotRevision != 0 || *child.Bindings[0].Entries[1].Source.SourceTrackID != tracks["b"].ID {
		t.Fatal("initial rule state or order incorrect")
	}
	if replay := decodePlaylist(expect("POST", "/playlists", childInput, "create-child", 201, "Playlist")); replay.ID != child.ID {
		t.Fatal("create replay changed identity")
	}
	parent := decodePlaylist(expect("POST", "/playlists", playlists.CreatePlaylistRequest{Name: "Parent", Bindings: []playlists.PlaylistBinding{{Kind: "playlist", TargetID: &child.ID}, local("c")}}, "create-parent", 201, "Playlist"))
	path := "/playlists/" + parent.ID
	build := func(key string, revision int64) int64 {
		t.Helper()
		w := expect("POST", path+"/rebuild", map[string]int64{"expectedRuleRevision": revision}, key, 200, "PlaylistSnapshotReceipt")
		if bytes.Contains(w.Body.Bytes(), []byte(`"entries"`)) {
			t.Fatal("rebuild expanded all entries")
		}
		var out struct {
			Revision int64 `json:"revision"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out.Revision
	}
	read := func(revision int64) playlists.AggregatePage {
		t.Helper()
		w := expect("GET", path+"/snapshots/"+strconv.FormatInt(revision, 10), nil, "", 200, "PlaylistSnapshotPage")
		var out playlists.AggregatePage
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	firstRevision := build("build-first", 1)
	first := read(firstRevision)
	if first.Total != 3 || len(first.Items[0].Provenance) != 2 || len(first.Items[0].Provenance[0].BindingPath) != 2 || first.Items[0].Provenance[0].OriginPlaylistID != child.ID {
		t.Fatal("nested duplicate provenance lost")
	}
	private, err := store.CreatePlaylist(ctx, "other", playlists.CreatePlaylistRequest{Name: "Private", Bindings: []playlists.PlaylistBinding{local("private")}}, "private-playlist")
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/hidden-rules", "/snapshots/1", "/changes"} {
		expect("GET", "/playlists/"+private.ID+suffix, nil, "", 404, "Error")
	}
	expect("POST", "/playlists", playlists.CreatePlaylistRequest{Name: "Private source", Bindings: []playlists.PlaylistBinding{local("private")}}, "forbidden-source", 404, "Error")
	expect("POST", "/playlists", map[string]any{"name": "Missing bindings"}, "missing-bindings", 400, "Error")
	expect("POST", "/playlists", childInput, "", 400, "Error")
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }} {
		w := request("POST", "/playlists", childInput, "denied-create", change)
		if w.Code < 400 {
			t.Fatal("playlist mutation bypassed authentication")
		}
	}
	for _, suffix := range []string{"?limit=0", "?limit=101", "?after=", "?limit=1&limit=2", "?unknown=true"} {
		expect("GET", "/playlists"+suffix, nil, "", 400, "Error")
	}
	expect("GET", "/playlists?limit=1", nil, "", 200, "Playlists")
	expect("POST", path+"/rebuild", struct{}{}, "no-expected-revision", 400, "Error")
	expect("POST", path+"/rebuild", map[string]int{"expectedRuleRevision": 2}, "stale-rebuild", 409, "Error")
	expect("PUT", "/playlists/"+child.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: child.Name, Bindings: []playlists.PlaylistBinding{{Kind: "playlist", TargetID: &parent.ID}}}, "reject-cycle", 409, "Error")
	source := local("a").Entries[0].Source
	hideInput := map[string]any{"expectedRevision": 0, "source": source}
	w := expect("POST", path+"/hidden-rules", hideInput, "hide-anchor", 200, "PlaylistHiddenRules")
	var hidden playlists.HiddenRules
	_ = json.Unmarshal(w.Body.Bytes(), &hidden)
	if hidden.Revision != 1 || hidden.Items[0].Scope != "allSources" || read(build("build-hidden", 1)).Total != 2 {
		t.Fatal("default hidden scope failed")
	}
	expect("POST", path+"/hidden-rules", map[string]any{"expectedRevision": 1, "source": source, "scope": nil}, "null-hide-scope", 400, "Error")
	expect("POST", path+"/hidden-rules", map[string]any{"source": source}, "missing-hide-revision", 400, "Error")
	linked, err := store.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks["a"].ID, tracks["b"].ID}}, "link-after-hide")
	if err != nil {
		t.Fatal(err)
	}
	visible := read(build("build-expanded", 1))
	if visible.Total != 2 || len(visible.NeedsConfirmation) != 1 || *visible.Items[0].Source.SourceTrackID != tracks["b"].ID {
		t.Fatal("association silently expanded hidden intent")
	}
	rulePath := path + "/hidden-rules/" + hidden.Items[0].ID
	expect("PUT", rulePath, map[string]any{"expectedRevision": 1, "active": true}, "missing-confirm", 400, "Error")
	expect("PUT", rulePath, playlists.ChangeHiddenRequest{ExpectedRevision: 1, Active: true, Confirm: true}, "confirm-hide", 200, "PlaylistHiddenRules")
	if read(build("build-confirmed", 1)).Total != 1 {
		t.Fatal("confirmed hide did not expand")
	}
	if _, err = store.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: linked.Revision, SourceTrackID: tracks["a"].ID}, "unlink-hidden-anchor"); err != nil {
		t.Fatal(err)
	}
	if read(build("build-unlinked", 1)).Total != 2 {
		t.Fatal("unlink extended hidden intent to every result")
	}
	edited := decodePlaylist(expect("PUT", path, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: parent.Name, Bindings: parent.Bindings[1:]}, "remove-child-binding", 200, "Playlist"))
	if edited.RuleRevision != 2 || edited.HideRevision != 2 {
		t.Fatal("independent revisions were conflated")
	}
	if after := read(build("build-without-child", 2)); after.Total != 1 || len(after.Items[0].Provenance) != 1 {
		t.Fatal("removing one binding removed other contribution")
	}
	if old := read(firstRevision); old.Total != 3 || old.Items[0].ID != first.Items[0].ID {
		t.Fatal("historical snapshot was rewritten")
	}
	if build("build-first", 1) != firstRevision {
		t.Fatal("rebuild idempotency reinterpreted later rules")
	}
	expect("POST", path+"/hidden-rules", hideInput, "hide-anchor", 200, "PlaylistHiddenRules")
	expect("PUT", rulePath, playlists.ChangeHiddenRequest{ExpectedRevision: 1, Active: false}, "stale-hide-change", 409, "Error")
	expect("GET", path+"/changes?after=0&limit=1", nil, "", 200, "PlaylistChanges")
	for _, suffix := range []string{"/changes?after=-1", "/changes?after=x", "/snapshots/0", "/snapshots/01", "/snapshots/1?offset=20001"} {
		expect("GET", path+suffix, nil, "", 400, "Error")
	}
}
