package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

func TestMusicLibraryMetadataWithoutSource(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	server.RegisterMusicLibrary(store)
	cookie, session := login(t, server)
	call := func(method, path string, data any, key string, status int, dest any) {
		t.Helper()
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/v1/library/"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		if dest != nil {
			if err = json.Unmarshal(w.Body.Bytes(), dest); err != nil {
				t.Fatal(err)
			}
		}
	}
	var a music.Album
	call("POST", "albums", map[string]any{"name": "Album", "externalId": "CAT-01", "publisher": "Label", "releaseDate": "2026-03-18"}, "create-album-one", 201, &a)
	in := map[string]any{"title": "First", "coverPath": "covers/cover.png", "artistNames": []string{"Singer"}, "albumIds": []string{a.ID}, "genre": "Jazz", "rating": 4, "year": 2002}
	var track, replay music.Track
	call("POST", "tracks", in, "create-track-one", 201, &track)
	call("POST", "tracks", in, "create-track-one", 201, &replay)
	if track.ID != replay.ID || track.SelectedSource != nil || len(track.Sources) != 0 || len(track.Artists) != 1 || len(track.Albums) != 1 || track.Rating == nil || *track.Rating != 4 {
		t.Fatalf("unexpected track: %+v", track)
	}
	var page music.TrackPage
	call("POST", "tracks", map[string]any{"title": "First"}, "create-track-two", 201, nil)
	call("GET", "tracks?limit=1", nil, "", 200, &page)
	if len(page.Items) != 1 || page.NextAfter == nil {
		t.Fatal(page)
	}
	var next music.TrackPage
	call("GET", "tracks?after="+*page.NextAfter, nil, "", 200, &next)
	if len(next.Items) != 1 || next.Items[0].ID == page.Items[0].ID {
		t.Fatal(next)
	}
	call("GET", "tracks?artistId="+track.Artists[0].ID, nil, "", 200, &next)
	if len(next.Items) != 1 || next.Items[0].ID != track.ID {
		t.Fatal(next)
	}
	in["rating"] = 6
	in["expectedRevision"] = 1
	call("PUT", "tracks/"+track.ID, in, "update-track-one", 400, nil)
	in["rating"] = 5
	call("PUT", "tracks/"+track.ID, in, "update-track-one", 200, &replay)
	if replay.Revision != 2 || *replay.Rating != 5 {
		t.Fatal(replay)
	}
	in["expectedRevision"] = 2
	call("PUT", "tracks/"+track.ID, in, "identical-metadata", 200, &track)
	if track.Revision != replay.Revision || track.UpdatedAt != replay.UpdatedAt {
		t.Fatal("unchanged track was rewritten")
	}
	call("GET", "tracks?after="+*page.NextAfter, nil, "", 410, nil)
	in["expectedRevision"] = 1
	call("PUT", "tracks/"+track.ID, in, "stale-track-update", 409, nil)
	call("POST", "tracks", map[string]any{"title": "Orphan", "albumIds": []string{"missing"}}, "missing-album-ref", 404, nil)
	if _, err := store.MusicTrack(t.Context(), "other", track.ID); err == nil {
		t.Fatal("cross-owner read")
	}
}
