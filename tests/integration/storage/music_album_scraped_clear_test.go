package storage_test

import (
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestClearAlbumScrapedFieldsPreservesLibrary(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	source := visibilityScan(t, s, "admin", "local", localizedObservation("one", "Local song", []string{"Local singer"}, 210000))[0]
	local, err := s.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := local.Albums[0]
	publisher := "My publisher"
	album.Name, album.Publisher = "My album", &publisher
	album.SourceLinks = []music.AlbumSourceLink{{PluginID: "fixture", URL: "https://example.com/album"}}
	album, err = s.SaveMusicAlbum(ctx, "admin", album.ID, album.Revision, album.AlbumFields, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	manual := metadataTrack(t, s, "My own music", album.ID)
	other, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Other album"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	applyAlbumTracks(t, s, other.ID, []scraper.Track{{Title: "Untouched"}})
	old := albumBundleFields([]scraper.Track{{Title: "Old release song", ExternalID: "old"}})
	for i := range old {
		old[i].Item.ID = "older-release"
	}
	if _, err := s.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, album.Revision, old, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	trackByTitle := func(title string) music.Track {
		t.Helper()
		rows := albumRows(t, s, album.ID, title)
		if len(rows) != 1 || rows[0].Title != title {
			t.Fatalf("expected one %q, got %+v", title, rows)
		}
		return rows[0]
	}
	stale := trackByTitle("Old release song")
	var tracks []scraper.Track
	for i, title := range []string{"Local song", "Unused", "Edited", "Favorite", "Playlist", "Shared", "Merged", "Cover preference"} {
		tracks = append(tracks, scraper.Track{Title: title, Number: i + 1})
	}
	fields := albumBundleFields(tracks)
	fields = append(fields, works.ScrapedField{Field: "artwork", PluginID: "fixture", CandidateToken: "cover-token", Item: scraper.Item{ID: "release", Title: "Cover"}})
	revision, err := s.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, album.Revision+1, fields, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	removed := []music.Track{stale, trackByTitle("Unused")}
	edited, fav, playlist := trackByTitle("Edited"), trackByTitle("Favorite"), trackByTitle("Playlist")
	shared, merged, cover := trackByTitle("Shared"), trackByTitle("Merged"), trackByTitle("Cover preference")
	if _, err := s.SaveMusicTrack(ctx, "admin", edited.ID, edited.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: "My edit"}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PutFavorite(ctx, "admin", fav.ID); err != nil {
		t.Fatal(err)
	}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,position) VALUES('admin','list','binding','entry',?,0)", playlist.ID)
	exec("INSERT INTO music_manual_album_tracks(album_id,track_id,position) VALUES(?,?,1)", other.ID, shared.ID)
	exec("INSERT INTO music_album_merged_slots(album_id,plugin_id,release_id,slot_key,track_id,preferred) VALUES(?,'fixture','release','merged-slot',?,1)", album.ID, merged.ID)
	exec("INSERT INTO recording_cover_preferences(owner,recording_id,revision,object_id) VALUES('admin',?,1,'chosen-art')", cover.ID)
	number := func(query string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := s.DB.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if n := number("SELECT count(*) FROM music_album_scraped_track_refs WHERE track_id=?", stale.ID); n != 0 {
		t.Fatal("expected an old placeholder outside the current release")
	}
	before := number("SELECT revision FROM library_revision")
	if got, err := s.ClearMusicAlbumScrapedFields(ctx, "admin", album.ID, revision, "clear-current"); err != nil || got != revision+1 {
		t.Fatalf("clear returned %d: %v", got, err)
	}
	if got, err := s.ClearMusicAlbumScrapedFields(ctx, "admin", album.ID, revision+1, "clear-again"); err != nil || got != revision+1 {
		t.Fatalf("empty clear returned %d: %v", got, err)
	}
	if got := number("SELECT revision FROM library_revision"); got != before+1 {
		t.Fatal("unexpected library revision", before, got)
	}
	for _, track := range removed {
		if number("SELECT count(*) FROM music_tracks WHERE id=?", track.ID) != 0 || number("SELECT count(*) FROM recordings WHERE id=?", track.ID) != 0 {
			t.Fatal("left unused placeholder or recording", track.ID)
		}
	}
	if n := number("SELECT count(*) FROM music_album_scraped_track_refs WHERE album_id=?", album.ID); n != 0 {
		t.Fatal("left album references", n)
	}
	for _, check := range []struct {
		id     string
		fields int
	}{{album.ID, 0}, {other.ID, 2}} {
		if got, err := s.MusicAlbumScrapedFields(ctx, "admin", check.id); err != nil || len(got) != check.fields {
			t.Fatalf("wrong fields for %s: %+v, %v", check.id, got, err)
		}
	}
	if n := number("SELECT count(*) FROM music_album_merged_slots WHERE album_id=? AND track_id=?", album.ID, merged.ID); n != 1 {
		t.Fatal("lost the manual merge")
	}
	if got, err := s.MusicAlbum(ctx, "admin", album.ID); err != nil || got.OriginalName != album.OriginalName || !reflect.DeepEqual(got.AlbumFields, album.AlbumFields) {
		t.Fatal("album fields changed", got, err)
	}
	for _, track := range []music.Track{local, manual, fav, playlist, shared, merged, cover} {
		if got := trackByTitle(track.Title); got.ID != track.ID || len(got.Sources) != len(track.Sources) {
			t.Fatal("protected track changed", got)
		}
	}
	if got := trackByTitle("My edit"); got.ID != edited.ID {
		t.Fatal("lost the edited track", got)
	}
	if rows := albumRows(t, s, other.ID, "Shared"); len(rows) != 1 || rows[0].ID != shared.ID {
		t.Fatal("lost another album's membership", rows)
	}
}

func TestClearAlbumScrapedFieldsRollsBack(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Album"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	applyAlbumTracks(t, s, album.ID, []scraper.Track{{Title: "Imported"}})
	if _, err := s.DB.ExecContext(ctx, "CREATE TRIGGER reject_clear BEFORE DELETE ON music_album_scraped_fields BEGIN SELECT RAISE(ABORT,'injected clear failure'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClearMusicAlbumScrapedFields(ctx, "admin", album.ID, 2, "rollback-clear"); err == nil {
		t.Fatal("injected failure was ignored")
	}
	saved, err := s.MusicAlbumScrapedFields(ctx, "admin", album.ID)
	if err != nil || len(saved) != 2 || len(albumRows(t, s, album.ID, "Imported")) != 1 {
		t.Fatal("partially cleared after failure", saved, err)
	}
	if _, err := s.DB.ExecContext(ctx, "DROP TRIGGER reject_clear"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.ClearMusicAlbumScrapedFields(ctx, "admin", album.ID, 2, "rollback-clear"); err != nil || got != 3 {
		t.Fatalf("clear after rollback returned %d: %v", got, err)
	}
}
