package api_test

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestMergeTracksInAlbumRequiresRevisionAndReplays(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	server.RegisterMusicLibrary(store)
	cookie, session := login(t, server)
	album, err := store.SaveMusicAlbum(t.Context(), "admin", "", 0, music.AlbumFields{Name: "Album"}, "create-manual-merge-album")
	if err != nil {
		t.Fatal(err)
	}
	var members []music.MergeTrackMember
	for _, title := range []string{"First", "Second"} {
		input := music.TrackInput{TrackFields: music.TrackFields{Title: title}, AlbumIDs: []string{album.ID}}
		item, err := store.SaveMusicTrack(t.Context(), "admin", "", 0, input, "create-track-"+title)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, music.MergeTrackMember{TrackID: item.ID, ExpectedRevision: item.Revision})
	}
	album, err = store.MusicAlbum(t.Context(), "admin", album.ID)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.MusicTracks(t.Context(), "admin", music.Query{AlbumID: album.ID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	reqBody := map[string]any{"targetTrackId": members[0].TrackID, "tracks": members, "expectedAlbumRevision": album.Revision}
	request := func(key string, csrf bool, status int) []byte {
		t.Helper()
		raw, err := json.Marshal(reqBody)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/v1/library/albums/"+album.ID+"/merge-tracks", bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("unexpected merge response: %d %s", w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	request("reject-missing-revision", true, 400)
	reqBody["expectedMatchingRevision"] = page.MatchingRevision
	request("reject-missing-csrf", false, 403)
	reqBody["targetTrackId"] = "outside-selection"
	request("reject-invalid-target", true, 400)
	reqBody["targetTrackId"] = members[0].TrackID
	raw, _ := json.Marshal(reqBody)
	testkit.ValidateHTTP(t, "MergeMusicAlbumTracks", raw)
	saved := request("merge-selected-tracks", true, 200)
	testkit.ValidateHTTP(t, "MusicTrack", saved)
	if !bytes.Equal(saved, request("merge-selected-tracks", true, 200)) {
		t.Fatal("merge replay changed the response")
	}
}
