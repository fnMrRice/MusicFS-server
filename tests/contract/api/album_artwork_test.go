package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
)

type albumArtPlugin struct {
	albumScraperFixture
	loads int
}

func (p *albumArtPlugin) OpenArtwork(context.Context, string) (io.ReadCloser, string, error) {
	p.loads++
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	img.Set(0, 0, color.NRGBA{R: uint8(p.loads), A: 255})
	var data bytes.Buffer
	err := png.Encode(&data, img)
	return io.NopCloser(&data), "image/png", err
}

func TestMusicAlbumArtworkHTTP(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	server.RegisterMusicLibrary(store)
	plugin := &albumArtPlugin{}
	scrapers, err := onlinescrape.New([]scraper.Plugin{plugin})
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterAlbumArtwork(onlinescrape.NewAlbumArtwork(t.TempDir(), scrapers, store, nil))
	cookie, session := login(t, server)
	request := func(method, path, body, key string, signed, csrf bool, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if signed {
			r.AddCookie(cookie)
		}
		if csrf {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	const collection = "/api/v1/library/albums"
	const upstream = "https://example.com/album.png"
	var album music.Album
	created := request("POST", collection, `{"name":"Image album","coverPath":"`+upstream+`"}`, "create-album-art", true, true, 201)
	if err = json.Unmarshal(created.Body.Bytes(), &album); err != nil {
		t.Fatal(err)
	}
	detail := collection + "/" + album.ID
	cover := detail + "/artwork"
	if album.CoverPath == nil || *album.CoverPath != cover {
		t.Fatalf("Unexpected public cover path: %+v", album.CoverPath)
	}
	track, err := store.SaveMusicTrack(t.Context(), "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: "Song"}, AlbumIDs: []string{album.ID}}, "track-artwork-test")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{collection, detail, "/api/v1/library/tracks", "/api/v1/library/tracks/" + track.ID} {
		data := request("GET", path, "", "", true, false, 200).Body.String()
		if strings.Contains(data, upstream) || !strings.Contains(data, cover) {
			t.Fatalf("Unexpected album reference from %s: %s", path, data)
		}
	}
	request("GET", cover, "", "", false, false, 401)
	request("GET", cover+"?unexpected=1", "", "", true, false, 400)
	first := request("GET", cover, "", "", true, false, 200)
	if first.Header().Get("Content-Type") != "image/png" || first.Header().Get("Cache-Control") != "private, no-cache" || plugin.loads != 1 {
		t.Fatalf("Unexpected image delivery: %v, loads=%d", first.Header(), plugin.loads)
	}
	request("POST", cover+"/refresh", "{}", "", true, false, 403)
	request("POST", cover+"/refresh", "{}", "", true, true, 204)
	second := request("GET", cover, "", "", true, false, 200)
	if bytes.Equal(first.Body.Bytes(), second.Body.Bytes()) || plugin.loads != 2 {
		t.Fatalf("Refresh failed to replace the cached image: loads=%d", plugin.loads)
	}
	other, err := store.SaveMusicAlbum(t.Context(), "other", "", 0, music.AlbumFields{Name: "Private"}, "private-album-art")
	if err != nil {
		t.Fatal(err)
	}
	request("GET", collection+"/"+other.ID+"/artwork", "", "", true, false, 404)
	request("PUT", detail, `{"name":"Renamed","expectedRevision":1,"coverPath":"`+cover+`"}`, "rename-album-art", true, true, 200)
	saved, err := store.MusicAlbum(t.Context(), "admin", album.ID)
	if err != nil || saved.CoverPath == nil || *saved.CoverPath != upstream {
		t.Fatalf("Updating the name changed the stored artwork reference: %+v %v", saved.CoverPath, err)
	}
	request("POST", collection, `{"name":"Other","coverPath":"`+cover+`"}`, "reject-album-art", true, true, 400)
	const local = "/api/v1/media/local-art"
	response := request("PUT", detail, `{"name":"Local","expectedRevision":2,"coverPath":"`+local+`"}`, "update-local-art", true, true, 200)
	if !strings.Contains(response.Body.String(), local) {
		t.Fatal("Local media artwork path was rewritten")
	}
	const member = "/api/v1/source-tracks/member/online-artwork"
	response = request("PUT", detail, `{"name":"Online member","expectedRevision":3,"coverPath":"`+member+`"}`, "update-member-art", true, true, 200)
	if !strings.Contains(response.Body.String(), cover) || strings.Contains(response.Body.String(), member) {
		t.Fatal("The online member artwork did not use the album cache")
	}
}
