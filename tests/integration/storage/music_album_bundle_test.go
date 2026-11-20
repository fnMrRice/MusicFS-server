package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func albumBundleFields(tracks []scraper.Track) []works.ScrapedField {
	return []works.ScrapedField{
		{Field: "albumDetails", PluginID: "fixture", CandidateToken: "details-token", Item: scraper.Item{ID: "release", Title: "Online album"}},
		{Field: "albumTracks", PluginID: "fixture", CandidateToken: "tracks-token", Item: scraper.Item{ID: "release", Title: "Online album", Artist: "Singer", Tracks: tracks}},
	}
}

func TestMusicAlbumBundleCreatesCompleteTracksAndPreservesSources(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	sources := visibilityScan(t, s, "admin", "local",
		visibilityObservation("one", "existing.wav", "Singer", "Scanned", "Jazz", "wav"),
		visibilityObservation("extra", "extra.wav", "Singer", "Scanned", "Jazz", "wav"))
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Original name"}, "create-bundle-album")
	if err != nil {
		t.Fatal(err)
	}
	for i, source := range sources {
		track, err := s.MusicTrackForSource(ctx, "admin", source.ID)
		if err != nil {
			t.Fatal(err)
		}
		title := []string{"Existing", "Extra"}[i]
		if _, err = s.SaveMusicTrack(ctx, "admin", track.ID, track.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: title}, ArtistNames: []string{"Singer"}, AlbumIDs: []string{album.ID}}, domain.NewID()); err != nil {
			t.Fatal(err)
		}
	}
	fields := albumBundleFields([]scraper.Track{{Title: "Z first", Disc: 1, Number: 1}, {Title: "Existing", Disc: 1, Number: 2}, {Title: "Duplicate", Disc: 1, Number: 3}, {Title: "Duplicate", Disc: 2, Number: 1}})
	if revision, err := s.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, 1, fields, "select-whole-album"); err != nil || revision != 2 {
		t.Fatalf("bundle selection: %d %v", revision, err)
	}
	query := music.Query{Limit: 2, AlbumID: album.ID}
	first, err := s.MusicTracks(ctx, "admin", query)
	if err != nil || len(first.Items) != 2 || first.Items[0].Title != "Z first" || first.Items[1].Title != "Existing" || first.Items[0].SelectedSource != nil || first.Items[1].SelectedSource == nil || first.NextAfter == nil {
		t.Fatalf("first album page: %+v %v", first, err)
	}
	firstID, sourcedID := first.Items[0].ID, first.Items[1].ID
	query.After = *first.NextAfter
	second, err := s.MusicTracks(ctx, "admin", query)
	if err != nil || len(second.Items) != 2 || second.Items[0].Title != "Duplicate" || second.Items[1].Title != "Duplicate" || second.Items[0].ID == second.Items[1].ID || second.NextAfter == nil {
		t.Fatalf("second album page: %+v %v", second, err)
	}
	query.After = *second.NextAfter
	third, err := s.MusicTracks(ctx, "admin", query)
	if err != nil || len(third.Items) != 1 || third.Items[0].Title != "Extra" || third.NextAfter != nil {
		t.Fatalf("extra source was lost: %+v %v", third, err)
	}
	if _, err = s.SaveMusicTrack(ctx, "admin", firstID, first.Items[0].Revision, music.TrackInput{TrackFields: music.TrackFields{Title: "Edited by user"}, AlbumIDs: []string{album.ID}}, "edit-imported-track"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, 2, fields, "rescrape-whole-album"); err != nil {
		t.Fatal(err)
	}
	page, err := s.MusicTracks(ctx, "admin", music.Query{Limit: 20, AlbumID: album.ID})
	if err != nil || len(page.Items) != 5 || page.Items[0].ID != firstID || page.Items[0].Title != "Edited by user" || page.Items[1].ID != sourcedID || page.Items[1].SelectedSource == nil {
		t.Fatalf("rescrape changed identity or user metadata: %+v %v", page, err)
	}
	sourced, err := s.MusicTracks(ctx, "admin", music.Query{Limit: 20, AlbumID: album.ID, SourceOnly: true})
	if err != nil || len(sourced.Items) != 2 {
		t.Fatalf("source-only leaked imported metadata: %+v %v", sourced, err)
	}
	updated, err := s.SaveMusicAlbum(ctx, "admin", album.ID, 3, music.AlbumFields{Name: "Display alias"}, "rename-bundle-album")
	if err != nil || updated.OriginalName != "Original name" || updated.Name != "Display alias" {
		t.Fatalf("original name changed: %+v %v", updated, err)
	}
	invalid := albumBundleFields([]scraper.Track{{Title: "Should not be saved"}})
	invalid[1].Item.ID = "another-release"
	if _, err = s.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, 4, invalid, "reject-other-release"); err == nil {
		t.Fatal("different releases were accepted")
	}
	invalid = albumBundleFields([]scraper.Track{{Title: "Valid first"}, {Title: ""}})
	if _, err = s.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, 4, invalid, "reject-partial-tracks"); err == nil {
		t.Fatal("invalid track list was partially accepted")
	}
	page, err = s.MusicTracks(ctx, "admin", music.Query{Limit: 20, AlbumID: album.ID})
	if err != nil || len(page.Items) != 5 {
		t.Fatalf("failed bundle published partial tracks: %+v %v", page, err)
	}
}
