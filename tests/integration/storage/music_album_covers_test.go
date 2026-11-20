package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestMusicAlbumCoverPriorityAndVisibleSourceFallback(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	original := "https://example.com/original.png"
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Album", CoverPath: &original}, "create-cover-album")
	if err != nil {
		t.Fatal(err)
	}
	expect := func(want string) {
		t.Helper()
		item, err := s.MusicAlbum(ctx, "admin", album.ID)
		if err != nil || want == "" && item.CoverPath != nil || want != "" && (item.CoverPath == nil || *item.CoverPath != want) {
			t.Fatalf("album cover: %+v, want %q, %v", item, want, err)
		}
		page, err := s.MusicAlbums(ctx, "admin", music.Query{Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range page.Items {
			if entry.ID == album.ID && (want == "" && entry.CoverPath != nil || want != "" && (entry.CoverPath == nil || *entry.CoverPath != want)) {
				t.Fatalf("album list cover: %+v, want %q", entry, want)
			}
		}
	}
	selectImage := func(field, url string) {
		t.Helper()
		current, err := s.MusicAlbum(ctx, "admin", album.ID)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.SelectMusicAlbumScrapedField(ctx, "admin", album.ID, current.Revision, works.ScrapedField{Field: field, PluginID: "fixture", CandidateToken: domain.NewID(), Item: scraper.Item{ID: "release", Title: "Album", ImageURL: url}}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
	}
	expect(original)
	selectImage("albumDetails", "https://example.com/details.png")
	expect("https://example.com/details.png")
	selectImage("artwork", "https://example.com/artwork.png")
	expect("https://example.com/artwork.png")
	selectImage("artwork", "javascript:alert(1)")
	expect("https://example.com/details.png")
	selectImage("albumDetails", "https://user:password@example.com/invalid.png")
	expect(original)
	visible := visibilityObservation("visible", "visible/song.wav", "Singer", "Scanned", "Rock", "wav")
	hidden := visibilityObservation("hidden", "hidden/song.wav", "Singer", "Scanned", "Rock", "wav")
	sources := visibilityScan(t, s, "admin", "local", visible, hidden)
	art := libraryArtwork(t, s, "admin", sources[0])
	contentSelect(t, s, art, 0, false)
	hiddenArt := libraryArtwork(t, s, "admin", sources[1])
	contentSelect(t, s, hiddenArt, 0, false)
	visibilityPackage(t, s, "admin", "local", "asmr", "directory", hidden)
	manual := "https://example.com/member.png"
	for i, source := range sources {
		track, err := s.MusicTrackForSource(ctx, "admin", source.ID)
		if err != nil {
			t.Fatal(err)
		}
		cover := manual
		if i == 1 {
			cover = "/api/v1/media/" + hiddenArt.ID
		}
		if _, err = s.SaveMusicTrack(ctx, "admin", track.ID, track.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: track.Title, CoverPath: &cover}, AlbumIDs: []string{album.ID}}, domain.NewID()); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := s.MusicAlbum(ctx, "admin", album.ID)
	unsafePath := "D:\\Music\\private-cover.jpg"
	if _, err = s.SaveMusicAlbum(ctx, "admin", album.ID, current.Revision, music.AlbumFields{Name: "Album", CoverPath: &unsafePath}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	expect(manual)
	track, _ := s.MusicTrackForSource(ctx, "admin", sources[0].ID)
	if _, err = s.SaveMusicTrack(ctx, "admin", track.ID, track.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: track.Title, CoverPath: &unsafePath}, AlbumIDs: []string{album.ID}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	expect("/api/v1/media/" + art.ID)
	foreign := visibilityScan(t, s, "other", "private", visible)[0]
	foreignArt := libraryArtwork(t, s, "other", foreign)
	current, _ = s.MusicAlbum(ctx, "admin", album.ID)
	foreignPath := "/api/v1/media/" + foreignArt.ID
	if _, err = s.SaveMusicAlbum(ctx, "admin", album.ID, current.Revision, music.AlbumFields{Name: "Album", CoverPath: &foreignPath}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	expect("/api/v1/media/" + art.ID)
	if _, err = s.MusicAlbum(ctx, "other", album.ID); err == nil {
		t.Fatal("album cover crossed owner boundary")
	}
	current, _ = s.MusicAlbum(ctx, "admin", album.ID)
	hiddenPath := "/api/v1/media/" + hiddenArt.ID
	if _, err = s.SaveMusicAlbum(ctx, "admin", album.ID, current.Revision, music.AlbumFields{Name: "Album", CoverPath: &hiddenPath}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	expect("/api/v1/media/" + art.ID)
	if err = s.InvalidateContent(ctx, "admin", art.ID); err != nil {
		t.Fatal(err)
	}
	expect("")
}
