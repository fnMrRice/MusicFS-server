package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/transform/thumbnail"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestRecordingCoverHTTPSelectionAndAuthorization(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	ctx := context.Background()
	provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
	if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan(provider), "cover-scan"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for _, name := range []string{"small", "large", "outside"} {
		items = append(items, library.Observation{Observation: pluginproto.Observation{
			ExternalID: name, RootID: provider, RelativePath: name + ".flac", DisplayName: name, ObservedVersion: "v1",
		}, Artists: []string{}})
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, len(items), nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, "admin", provider, "", 10)
	if err != nil || len(page.Items) != 3 {
		t.Fatal("missing fixture tracks", err)
	}
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	content := mediastore.NewContent(store, store, files, thumbnail.New())
	objects, tracks := map[string]mediastore.ContentObject{}, map[string]library.Track{}
	for _, track := range page.Items {
		tracks[track.DisplayName] = track
		side := 2
		if track.DisplayName == "large" {
			side = 12
		}
		var data bytes.Buffer
		if err = png.Encode(&data, image.NewRGBA(image.Rect(0, 0, side, side))); err != nil {
			t.Fatal(err)
		}
		metadata := mediastore.ContentMetadata{Target: mediastore.ContentTarget{
			SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version,
		}, Field: "artwork", Origin: mediastore.Origin{Kind: "upload"}}
		object, uploadErr := content.Upload(ctx, "admin", metadata, "image/png", domain.NewID(), &data)
		if uploadErr != nil {
			t.Fatal(uploadErr)
		}
		if _, err = store.SelectContent(ctx, "admin", mediastore.SelectRequest{
			Target: metadata.Target, ObjectID: object.ID,
		}, domain.NewID()); err != nil {
			t.Fatal(err)
		}
		objects[track.DisplayName] = object
	}
	joined, err := store.ManualLink(ctx, "admin", matching.LinkRequest{
		SourceTrackIDs: []string{tracks["small"].ID, tracks["large"].ID},
	}, domain.NewID())
	if err != nil || joined.RecordingID == nil {
		t.Fatal("missing recording", err)
	}
	path := "/api/v1/recordings/" + *joined.RecordingID + "/cover"
	request := func(method string, body any, key string, status int, mutate func(*http.Request)) []byte {
		t.Helper()
		raw, _ := json.Marshal(body)
		r := httptest.NewRequest(method, path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if mutate != nil {
			mutate(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: got %d want %d: %s", method, w.Code, status, w.Body.String())
		}
		model := "RecordingCover"
		if status >= 400 {
			model = "Error"
		}
		testkit.ValidateHTTP(t, model, w.Body.Bytes())
		return w.Body.Bytes()
	}
	read := func() library.RecordingCover {
		t.Helper()
		var result library.RecordingCover
		if err := json.Unmarshal(request("GET", nil, "", 200, nil), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	initial := read()
	if initial.CoverObjectID == nil || *initial.CoverObjectID != objects["large"].ID || initial.ManualObjectID != nil || len(initial.Candidates) != 2 {
		t.Fatalf("automatic cover did not choose the largest image: %+v", initial)
	}
	valid := map[string]any{"expectedRevision": initial.Revision, "objectId": objects["small"].ID}
	request("GET", nil, "", 401, func(r *http.Request) { r.Header.Del("Cookie") })
	request("PUT", valid, "cover-choice", 403, func(r *http.Request) { r.Header.Del("X-CSRF-Token") })
	request("PUT", valid, "", 400, nil)
	for _, body := range []any{
		map[string]any{"expectedRevision": initial.Revision},
		map[string]any{"expectedRevision": nil, "objectId": nil},
		map[string]any{"expectedRevision": -1, "objectId": nil},
		map[string]any{"expectedRevision": initial.Revision, "objectId": 123},
		map[string]any{"expectedRevision": initial.Revision, "objectId": ""},
	} {
		request("PUT", body, domain.NewID(), 400, nil)
	}
	request("PUT", map[string]any{"expectedRevision": initial.Revision, "objectId": objects["outside"].ID}, domain.NewID(), 409, nil)
	first := request("PUT", valid, "cover-choice", 200, nil)
	if replay := request("PUT", valid, "cover-choice", 200, nil); !bytes.Equal(first, replay) {
		t.Fatal("idempotent selection response changed")
	}
	request("PUT", valid, "cover-stale", 409, nil)
	manual := read()
	if manual.CoverObjectID == nil || *manual.CoverObjectID != objects["small"].ID || manual.ManualObjectID == nil || *manual.ManualObjectID != objects["small"].ID {
		t.Fatalf("manual preference was not applied: %+v", manual)
	}
	grouped, err := store.ListGroupedTracks(ctx, "admin", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, track := range grouped.Items {
		if track.RecordingID == nil || *track.RecordingID != *joined.RecordingID {
			continue
		}
		found = true
		if track.CoverObjectID == nil || *track.CoverObjectID != objects["small"].ID {
			t.Fatal("grouped library omitted the manual cover")
		}
		for _, version := range track.Versions {
			if version.CoverObjectID == nil || *version.CoverObjectID != objects[version.DisplayName].ID {
				t.Fatal("recording choice overwrote a source cover")
			}
		}
	}
	if !found {
		t.Fatal("missing grouped recording")
	}
	request("PUT", map[string]any{"expectedRevision": manual.Revision, "objectId": nil}, "cover-auto", 200, nil)
	restored := read()
	if restored.ManualObjectID != nil || restored.CoverObjectID == nil || *restored.CoverObjectID != objects["large"].ID {
		t.Fatal("automatic cover was not restored")
	}
	if _, err = store.RecordingCover(ctx, "other", *joined.RecordingID); err == nil {
		t.Fatal("recording cover leaked to another owner")
	}
}
