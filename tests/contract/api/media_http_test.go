package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMediaHTTPPreviewSelectionPrivacyAndMissingFiles(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	targets := map[string]mediastore.ContentTarget{}
	for _, owner := range []string{"admin", "other"} {
		provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
		if owner == "other" {
			provider = "843558e1-a158-56ac-834a-baffd12bb5c5"
		}
		if _, _, err := store.Enqueue(ctx, owner, provider, testkit.ScanPlan(provider), "media-test-source"); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		item := library.Observation{Observation: pluginproto.Observation{ExternalID: "song", RootID: provider, RelativePath: "secret/media/source.wav", DisplayName: "song", ObservedVersion: "source-version", SizeBytes: 1}, Artists: []string{}}
		if err := store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{item})); err != nil {
			t.Fatal(err)
		}
		if err := store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
		page, err := store.ListTracks(ctx, owner, provider, "", 50)
		if err != nil || len(page.Items) != 1 {
			t.Fatal("missing source", err)
		}
		track := page.Items[0]
		targets[owner] = mediastore.ContentTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version}
	}
	mediaRoot := t.TempDir()
	files, err := mediastore.OpenFiles(mediaRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	service := mediastore.NewContent(store, store, files, thumbnail.New())
	server.RegisterMedia(service)
	private, err := service.Upload(ctx, "other", mediastore.ContentMetadata{Target: targets["other"], Field: "lyrics", Origin: mediastore.Origin{Kind: "upload"}}, "text/plain", "private-media-key", strings.NewReader("Private media text"))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string, body []byte, mime, key string, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", mime)
		r.Header.Set("Idempotency-Key", key)
		if mutate != nil {
			mutate(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if bytes.Contains(w.Body.Bytes(), []byte("secret/media")) || bytes.Contains(w.Body.Bytes(), []byte("Private media text")) || bytes.Contains(w.Body.Bytes(), []byte(`"relativePath"`)) {
			t.Fatal("private content or disk path leaked")
		}
		if w.Code >= 400 {
			testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
		}
		return w
	}
	uploadPath := func(target mediastore.ContentTarget, field, language string) string {
		q := url.Values{"sourceTrackId": {target.SourceTrackID}, "assetId": {target.AssetID}, "assetVersion": {target.AssetVersion}, "field": {field}, "language": {language}}
		return "/media?" + q.Encode()
	}
	path := uploadPath(targets["admin"], "lyrics", "ZH-CN")
	unknownPath := uploadPath(targets["admin"], "lyrics", "")
	unknownBody := []byte("Unknown language synthetic lyrics")
	unknownResponse := request("POST", unknownPath, unknownBody, "text/plain", "upload-unknown-language", nil)
	var unknown mediastore.ContentObject
	if unknownResponse.Code != 201 || json.Unmarshal(unknownResponse.Body.Bytes(), &unknown) != nil || unknown.Metadata.Language != "und" {
		t.Fatal("empty lyrics language was not canonicalized")
	}
	testkit.ValidateHTTP(t, "ContentObject", unknownResponse.Body.Bytes())
	unknownResponse = request("POST", uploadPath(targets["admin"], "lyrics", "UND"), unknownBody, "text/plain", "upload-unknown-language", nil)
	var unknownReplay mediastore.ContentObject
	if unknownResponse.Code != 201 || json.Unmarshal(unknownResponse.Body.Bytes(), &unknownReplay) != nil || unknownReplay.ID != unknown.ID {
		t.Fatal("empty and und languages did not share an idempotency identity")
	}
	lyrics := []byte(strings.Repeat("[00:01.00]合成歌词\n", 5000))
	if len(lyrics) <= 64<<10 {
		t.Fatal("fixture does not exceed ordinary control body budget")
	}
	for _, tc := range []struct {
		name, path, mime, key string
		body                  []byte
		status                int
		mutate                func(*http.Request)
	}{
		{"anonymous", path, "text/plain", "upload-check", lyrics, 401, func(r *http.Request) { r.Header.Del("Cookie") }},
		{"csrf", path, "text/plain", "upload-check", lyrics, 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }},
		{"origin", path, "text/plain", "upload-check", lyrics, 403, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }},
		{"no-mime", path, "", "upload-check", lyrics, 400, nil}, {"wrong-mime", path, "image/png", "upload-check", lyrics, 415, nil},
		{"no-key", path, "text/plain", "", lyrics, 400, nil}, {"empty", path, "text/plain", "upload-check", nil, 415, nil},
		{"private-target", uploadPath(targets["other"], "lyrics", ""), "text/plain", "upload-check", lyrics, 404, nil},
		{"fake-origin", path + "&origin=scraper", "text/plain", "upload-check", lyrics, 400, nil},
		{"fake-width", uploadPath(targets["admin"], "artwork", "") + "&width=8192", "image/png", "upload-check", testkit.ScraperPNG(), 400, nil},
		{"fake-height", uploadPath(targets["admin"], "artwork", "") + "&height=8192", "image/png", "upload-check", testkit.ScraperPNG(), 400, nil},
		{"filename", path + "&filename=..%2Fsecret.txt", "text/plain", "upload-check", lyrics, 400, nil},
		{"remote-url", path + "&url=https%3A%2F%2Fprivate.invalid", "text/plain", "upload-check", lyrics, 400, nil},
		{"duplicate-field", path + "&field=artwork", "text/plain", "upload-check", lyrics, 400, nil},
		{"lyrics-limit", path, "text/plain", "upload-check", bytes.Repeat([]byte("x"), (1<<20)+1), 415, nil},
		{"damaged-image", uploadPath(targets["admin"], "artwork", ""), "image/png", "upload-check", []byte("\x89PNG\r\n\x1a\ninvalid"), 415, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := request("POST", tc.path, tc.body, tc.mime, tc.key, tc.mutate)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	w := request("POST", path, lyrics, "text/plain; charset=utf-8", "upload-lyrics-key", nil)
	if w.Code != 201 {
		t.Fatalf("upload: %d %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "ContentObject", w.Body.Bytes())
	var first mediastore.ContentObject
	if json.Unmarshal(w.Body.Bytes(), &first) != nil || first.Metadata.Format != "lrc" || first.Metadata.Language != "zh-cn" || first.Metadata.Origin.Kind != "upload" || first.Metadata.Origin.InstanceID != "" {
		t.Fatal("media provenance or format missing")
	}
	for _, suffix := range []string{"", "/info"} {
		w = request("GET", "/media/"+private.ID+suffix, nil, "", "", nil)
		if w.Code != 404 {
			t.Fatal("private object accessible")
		}
	}
	w = request("GET", "/media/"+first.ID, nil, "", "", nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), lyrics) || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authorized media body/headers invalid")
	}
	selectionPath := "/source-tracks/" + targets["admin"].SourceTrackID + "/media-selection"
	readSelection := func() mediastore.Selection {
		t.Helper()
		w := request("GET", selectionPath, nil, "", "", nil)
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		testkit.ValidateHTTP(t, "MediaSelection", w.Body.Bytes())
		var selection mediastore.Selection
		if json.Unmarshal(w.Body.Bytes(), &selection) != nil {
			t.Fatal("invalid selection")
		}
		return selection
	}
	if s := readSelection(); s.Revision != 0 || len(s.Bindings) != 0 {
		t.Fatal("upload selected its preview automatically")
	}
	w = request("POST", path, lyrics, "text/plain", "upload-lyrics-key", nil)
	var replay mediastore.ContentObject
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &replay) != nil || replay.ID != first.ID {
		t.Fatal("upload did not replay")
	}
	w = request("POST", path, []byte("different lyrics"), "text/plain", "upload-lyrics-key", nil)
	if w.Code != 409 {
		t.Fatal("upload key reused with changed bytes")
	}
	selectBody := func(object string, revision int64) []byte {
		raw, _ := json.Marshal(map[string]any{"objectId": object, "assetId": targets["admin"].AssetID, "assetVersion": targets["admin"].AssetVersion, "expectedRevision": revision})
		return raw
	}
	w = request("PUT", selectionPath, selectBody(first.ID, 0), "application/json", "select-lyrics-key", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	testkit.ValidateHTTP(t, "MediaSelection", w.Body.Bytes())
	chosen := readSelection()
	if chosen.Revision != 1 || len(chosen.Bindings) != 1 || chosen.Bindings[0].ObjectID != first.ID || !chosen.Bindings[0].ManualLocked || !chosen.Bindings[0].Current {
		t.Fatal("explicit selection did not lock current content")
	}
	for _, bad := range []string{`{"objectId":"x","assetId":"a","assetVersion":"v"}`, `{"objectId":"x","assetId":"a","assetVersion":"v","expectedRevision":null}`, `{"objectId":"x","assetId":"a","assetVersion":"v","expectedRevision":1,"automatic":true}`, `{"objectId":"x","assetId":"a","assetVersion":"v","expectedRevision":1,"sourceTrackId":"other"}`} {
		if w := request("PUT", selectionPath, []byte(bad), "application/json", "select-invalid", nil); w.Code != 400 {
			t.Fatal("invalid selection request accepted")
		}
	}
	w = request("PUT", selectionPath, selectBody(private.ID, 1), "application/json", "private-object-key", nil)
	if w.Code != 404 {
		t.Fatal("foreign object selected")
	}
	w = request("PUT", selectionPath, selectBody(first.ID, 0), "application/json", "stale-selection-key", nil)
	if w.Code != 409 {
		t.Fatal("stale revision selected")
	}
	w = request("POST", path, []byte("Edited local lyrics"), "text/plain", "upload-edited-key", nil)
	var edited mediastore.ContentObject
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &edited) != nil {
		t.Fatal("edited text upload failed")
	}
	if s := readSelection(); s.Bindings[0].ObjectID != first.ID || s.Revision != 1 {
		t.Fatal("new preview replaced old selection")
	}
	w = request("POST", path, []byte("Preview that will be damaged"), "text/plain", "upload-damaged-key", nil)
	var damaged mediastore.ContentObject
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &damaged) != nil {
		t.Fatal("damaged fixture upload failed")
	}
	if err := os.WriteFile(filepath.Join(mediaRoot, "objects", damaged.ID), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	w = request("PUT", selectionPath, selectBody(damaged.ID, 1), "application/json", "select-damaged-key", nil)
	if w.Code != 409 {
		t.Fatalf("damaged preview was selected: %d %s", w.Code, w.Body.String())
	}
	if s := readSelection(); s.Revision != 1 || s.Bindings[0].ObjectID != first.ID || !s.Bindings[0].ManualLocked {
		t.Fatal("damaged preview replaced old content")
	}
	unlock := []byte(`{"field":"lyrics","language":"ZH-CN","expectedRevision":1}`)
	w = request("POST", selectionPath+"/unlock", unlock, "application/json", "unlock-lyrics-key", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if s := readSelection(); s.Revision != 2 || s.Bindings[0].ManualLocked || s.Bindings[0].ObjectID != first.ID {
		t.Fatal("unlock cleared old content")
	}
	w = request("PUT", selectionPath, selectBody(edited.ID, 2), "application/json", "select-edited-key", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	var cover bytes.Buffer
	if err := png.Encode(&cover, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	w = request("POST", uploadPath(targets["admin"], "artwork", ""), cover.Bytes(), "image/png", "upload-cover-key", nil)
	var artwork mediastore.ContentObject
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &artwork) != nil || artwork.Metadata.Format != "png" || artwork.Width != 2 || artwork.Height != 2 {
		t.Fatal("valid cover upload failed")
	}
	testkit.ValidateHTTP(t, "ContentObject", w.Body.Bytes())
	w = request("GET", "/media/"+artwork.ID+"/info", nil, "", "", nil)
	var artworkInfo mediastore.ContentObject
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &artworkInfo) != nil || artworkInfo.Width != 2 || artworkInfo.Height != 2 {
		t.Fatal("persisted cover dimensions were not exposed")
	}
	w = request("PUT", selectionPath, selectBody(artwork.ID, 3), "application/json", "select-cover-key", nil)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	if s := readSelection(); s.Revision != 4 || len(s.Bindings) != 2 {
		t.Fatal("independent field selection missing")
	}
	if err := os.Remove(filepath.Join(mediaRoot, "objects", edited.ID)); err != nil {
		t.Fatal(err)
	}
	w = request("GET", "/media/"+edited.ID, nil, "", "", nil)
	if w.Code != 409 {
		t.Fatal("missing content not marked unavailable")
	}
	s := readSelection()
	var unavailable bool
	for _, binding := range s.Bindings {
		if binding.ObjectID == edited.ID {
			unavailable = binding.State == "unavailable" && binding.ManualLocked && binding.Current
		}
	}
	if !unavailable {
		t.Fatal("missing content lost manual lock or became falsely available")
	}
	w = request("GET", "/media/"+first.ID, nil, "", "", nil)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), lyrics) {
		t.Fatal("older authorized object lost its content")
	}
	w = request("GET", "/media/"+first.ID+"/info", nil, "", "", nil)
	if w.Code != 200 {
		t.Fatal("authorized object information unavailable")
	}
	testkit.ValidateHTTP(t, "ContentObject", w.Body.Bytes())
}
