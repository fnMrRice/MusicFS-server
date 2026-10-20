package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAlbumsHTTPIdentityCoverRevisionPrivacyAndPagination(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	tracks := map[string][]library.Track{}
	for _, owner := range []string{"admin", "other"} {
		provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
		if owner == "other" {
			provider = "843558e1-a158-56ac-834a-baffd12bb5c5"
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "album-fixture-scan"); err != nil {
			t.Fatal(err)
		}
		scan, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		items := []library.Observation{}
		for _, id := range []string{"one", "two"} {
			items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: id, RootID: provider, RelativePath: "private-path/" + id + ".flac", DisplayName: id, ObservedVersion: "v1", SizeBytes: 100}, Artists: []string{}})
		}
		if err := store.Publish(ctx, scan, testkit.ScanPublication(scan, items)); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, scan, 2, nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 10)
		if err != nil || len(page.Items) != 2 {
			t.Fatal("missing fixture tracks", err)
		}
		tracks[owner] = page.Items
	}
	mediaRoot := t.TempDir()
	files, err := mediastore.OpenFiles(mediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	content := mediastore.NewContent(store, store, files)
	server.RegisterMedia(content)
	providers := library.New(store, jobs.New(store), []library.Runtime{{ID: "316664dc-7106-5acd-8df7-88c9d0d9baef", Owner: "admin"}, {ID: "843558e1-a158-56ac-834a-baffd12bb5c5", Owner: "other"}}, nil)
	server.RegisterAlbums(albums.New(store, content, providers))
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	artwork := func(owner string, index int, key string) mediastore.ContentObject {
		t.Helper()
		track := tracks[owner][index]
		object, err := content.Upload(ctx, owner, mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}, "image/png", key, bytes.NewReader(pngBytes.Bytes()))
		if err != nil {
			t.Fatal(err)
		}
		return object
	}
	art, outside, privateArt := artwork("admin", 0, "album-art-1"), artwork("admin", 1, "album-art-2"), artwork("other", 0, "album-art-private")
	private, err := store.CreateSourceAlbum(ctx, "other", albums.CreateSourceAlbumRequest{ProviderInstanceID: "843558e1-a158-56ac-834a-baffd12bb5c5", Name: "Private album", SourceTrackIDs: []string{tracks["other"][0].ID}}, "private-album-create")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body any, key string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if body != nil {
			var err error
			raw, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
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
		for _, secret := range []string{"private-path", `"owner"`, "Private album"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private album data leaked")
			}
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	w := request("POST", "/media/"+art.ID+"/thumbnail", map[string]any{}, "", nil)
	var thumbnail mediastore.DerivedObject
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &thumbnail) != nil {
		t.Fatalf("thumbnail: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "DerivedObject", w.Body.Bytes())
	w = request("POST", "/media/"+art.ID+"/thumbnail", map[string]any{}, "", nil)
	var replayThumbnail mediastore.DerivedObject
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &replayThumbnail) != nil || replayThumbnail.ID != thumbnail.ID {
		t.Fatal("thumbnail did not naturally replay")
	}
	w = request("GET", "/media/"+thumbnail.ID+"/info", nil, "", nil)
	if w.Code != 200 {
		t.Fatal("derived object info unavailable")
	}
	testkit.ValidateHTTP(t, "MediaObjectInfo", w.Body.Bytes())
	w = request("GET", "/media/"+thumbnail.ID, nil, "", nil)
	size, err := png.DecodeConfig(bytes.NewReader(w.Body.Bytes()))
	if w.Code != 200 || err != nil || size.Width != 2 || size.Height != 2 {
		t.Fatal("thumbnail enlarged its parent or failed delivery")
	}
	for _, tc := range []struct {
		id     string
		body   any
		status int
		change func(*http.Request)
	}{
		{privateArt.ID, map[string]any{}, 404, nil},
		{art.ID, map[string]any{"path": "bad"}, 400, nil},
		{art.ID, map[string]any{}, 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
	} {
		if w := request("POST", "/media/"+tc.id+"/thumbnail", tc.body, "", tc.change); w.Code != tc.status {
			t.Fatalf("thumbnail rejection: %d %s", w.Code, w.Body.String())
		}
	}
	create := func(source, key string) albums.SourceAlbum {
		t.Helper()
		w := request("POST", "/source-albums", map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": " 同名专辑 ", "sourceTrackIds": []string{source}}, key, nil)
		var a albums.SourceAlbum
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &a) != nil {
			t.Fatalf("create album: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "SourceAlbum", w.Body.Bytes())
		return a
	}
	one := create(tracks["admin"][0].ID, "album-one-create")
	two := create(tracks["admin"][1].ID, "album-two-create")
	if one.ID == two.ID || one.Name != "同名专辑" || one.Revision != 1 {
		t.Fatal("name used as identity or revision missing")
	}
	if again := create(tracks["admin"][0].ID, "album-one-create"); again.ID != one.ID {
		t.Fatal("create replay changed identity")
	}
	valid := map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": "Another", "sourceTrackIds": []string{tracks["admin"][0].ID}}
	for _, tc := range []struct {
		name   string
		body   any
		status int
		change func(*http.Request)
	}{
		{"auth", valid, 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", valid, 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", valid, 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"null-members", map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": "x", "sourceTrackIds": nil}, 400, nil},
		{"missing-members", map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": "x"}, 400, nil},
		{"private-member", map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": "x", "sourceTrackIds": []string{tracks["other"][0].ID}}, 404, nil},
		{"private-empty-provider", map[string]any{"providerInstanceId": "843558e1-a158-56ac-834a-baffd12bb5c5", "name": "x", "sourceTrackIds": []string{}}, 404, nil},
		{"unknown-empty-provider", map[string]any{"providerInstanceId": "ffe1a974-bec2-5351-b78f-6062ea893b84", "name": "x", "sourceTrackIds": []string{}}, 404, nil},
		{"duplicate", map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": "x", "sourceTrackIds": []string{tracks["admin"][0].ID, tracks["admin"][0].ID}}, 400, nil},
		{"path", map[string]any{"providerInstanceId": "316664dc-7106-5acd-8df7-88c9d0d9baef", "name": "x", "sourceTrackIds": []string{}, "path": "bad"}, 400, nil},
	} {
		w := request("POST", "/source-albums", tc.body, "album-test-"+tc.name, tc.change)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
	}
	if w := request("POST", "/source-albums", valid, "album-one-create", nil); w.Code != 409 {
		t.Fatal("same key changed album parameters")
	}
	seen := map[string]bool{}
	after := ""
	for i := range 2 {
		w := request("GET", "/source-albums?limit=1&after="+after, nil, "", nil)
		var page struct {
			Items []albums.SourceAlbum `json:"items"`
			Next  *string              `json:"nextAfter"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Items) != 1 {
			t.Fatal("invalid album page")
		}
		testkit.ValidateHTTP(t, "SourceAlbums", w.Body.Bytes())
		if seen[page.Items[0].ID] {
			t.Fatal("duplicate album page")
		}
		seen[page.Items[0].ID] = true
		if i == 0 {
			if page.Next == nil {
				t.Fatal("missing next album cursor")
			}
			after = *page.Next
		} else if page.Next != nil {
			t.Fatal("spurious next album cursor")
		}
	}
	for _, suffix := range []string{"?limit=0", "?limit=101", "?limit=", "?limit=1&limit=2", "?unknown=x", "?after=%ZZ"} {
		if w := request("GET", "/source-albums"+suffix, nil, "", nil); w.Code != 400 {
			t.Fatalf("invalid album query: %s", suffix)
		}
	}
	for _, path := range []string{"/source-albums/" + private.ID, "/source-albums/" + private.ID + "/cover-selection", "/source-albums/" + private.ID + "/changes"} {
		if w := request("GET", path, nil, "", nil); w.Code != 404 {
			t.Fatal("private album exposed")
		}
	}
	w = request("POST", "/releases", map[string]any{"name": "发行", "sourceAlbumIds": []string{one.ID, two.ID}}, "release-create", nil)
	var release albums.Release
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &release) != nil {
		t.Fatalf("create release: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "Release", w.Body.Bytes())
	for _, path := range []string{"/source-albums/" + one.ID, "/releases/" + release.ID} {
		w := request("GET", path+"/cover-selection", nil, "", nil)
		var initial albums.CoverSelection
		_ = json.Unmarshal(w.Body.Bytes(), &initial)
		if w.Code != 200 || initial.Revision != 0 || initial.ManualLocked || initial.Binding != nil {
			t.Fatal("implicit cover selection")
		}
		testkit.ValidateHTTP(t, "AlbumCoverSelection", w.Body.Bytes())
		body := map[string]any{"objectId": art.ID, "expectedRevision": 0, "expectedTargetRevision": 1}
		w = request("PUT", path+"/cover-selection", body, "cover-select-"+path, nil)
		var selected albums.CoverSelection
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &selected) != nil || !selected.ManualLocked || selected.Revision != 1 || selected.Binding.ObjectID != art.ID {
			t.Fatalf("cover selection: %d %s", w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, "AlbumCoverSelection", w.Body.Bytes())
		w = request("PUT", path+"/cover-selection", map[string]any{"objectId": privateArt.ID, "expectedRevision": 1, "expectedTargetRevision": 1}, domain.NewID(), nil)
		if w.Code != 404 {
			t.Fatal("private artwork accepted")
		}
	}
	coverPath := "/source-albums/" + one.ID + "/cover-selection"
	w = request("PUT", coverPath, map[string]any{"objectId": outside.ID, "expectedRevision": 1, "expectedTargetRevision": 1}, "outside-cover-select", nil)
	if w.Code != 400 {
		t.Fatal("nonmember cover accepted")
	}
	broken := artwork("admin", 0, "album-broken-preview")
	if err := os.WriteFile(filepath.Join(mediaRoot, "objects", broken.ID), []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	w = request("PUT", coverPath, map[string]any{"objectId": broken.ID, "expectedRevision": 1, "expectedTargetRevision": 1}, "broken-cover-select", nil)
	if w.Code != 409 {
		t.Fatalf("damaged artwork: %d %s", w.Code, w.Body.String())
	}
	if selection, err := store.ContentSelection(ctx, "admin", tracks["admin"][0].ID); err != nil || selection.Revision != 0 || len(selection.Bindings) != 0 {
		t.Fatal("album cover changed per-source media selection")
	}
	w = request("PUT", "/source-albums/"+one.ID, map[string]any{"name": one.Name, "sourceTrackIds": []string{}, "expectedRevision": 1}, "album-remove-members", nil)
	if w.Code != 200 {
		t.Fatalf("update album: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "SourceAlbum", w.Body.Bytes())
	w = request("GET", "/releases/"+release.ID, nil, "", nil)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &release) != nil || release.Revision != 2 {
		t.Fatal("release revision did not follow member album change")
	}
	w = request("PUT", "/releases/"+release.ID+"/cover-selection", map[string]any{"objectId": outside.ID, "expectedRevision": 1, "expectedTargetRevision": 1}, "stale-release-cover", nil)
	if w.Code != 409 {
		t.Fatal("stale release membership revision accepted")
	}
	for _, path := range []string{"/source-albums/" + one.ID, "/releases/" + release.ID} {
		w := request("GET", path+"/cover-selection", nil, "", nil)
		var selected albums.CoverSelection
		_ = json.Unmarshal(w.Body.Bytes(), &selected)
		if w.Code != 200 || selected.Binding == nil || selected.Binding.ObjectID != art.ID || !selected.ManualLocked || selected.Revision != 1 {
			t.Fatal("membership edit erased independent cover")
		}
		w = request("POST", path+"/cover-selection/unlock", map[string]any{"expectedRevision": 1}, "cover-unlock-"+path, nil)
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &selected) != nil || selected.ManualLocked || selected.Revision != 2 || selected.Binding.ObjectID != art.ID {
			t.Fatal("unlock erased cover")
		}
		testkit.ValidateHTTP(t, "AlbumCoverSelection", w.Body.Bytes())
		w = request("GET", path+"/changes?limit=1", nil, "", nil)
		if w.Code != 200 {
			t.Fatal("audit failed")
		}
		testkit.ValidateHTTP(t, "AlbumChanges", w.Body.Bytes())
	}
}
