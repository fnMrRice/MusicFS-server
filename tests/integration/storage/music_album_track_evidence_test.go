package storage_test

import (
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAutoAlbumEvidenceUsesSourceTagsAndUniqueArtistAliases(t *testing.T) {
	store := visibilityStore(t)
	ctx := t.Context()
	sources := visibilityScan(t, store, "admin", "local",
		localizedObservation("one", "First song", []string{"First artist"}, 120000),
		localizedObservation("two", "Second song", []string{"Second artist"}, 200000))
	tracks := map[string]music.Track{}
	for _, source := range sources {
		track, err := store.MusicTrackForSource(ctx, "admin", source.ID)
		if err != nil {
			t.Fatal(err)
		}
		tracks[track.Title] = track
		artist := track.Artists[0]
		names := []scraper.LocalizedTitle{{Language: "en", Title: "Shared alias"}}
		if track.Title == "First song" {
			names = append(names, scraper.LocalizedTitle{Language: "zh", Title: "歌手甲"})
		}
		if _, err := store.SaveMusicArtistNames(ctx, "admin", artist.ID, artist.Revision,
			music.ArtistNamesInput{LocalizedNames: names}, domain.NewID()); err != nil {
			t.Fatal(err)
		}
	}
	album := tracks["First song"].Albums[0]
	// 模拟现有误刮：真实成员保留了错误别名，另一首凭空创建为占位。
	previous := albumBundleFields([]scraper.Track{
		{Title: "First song", Artist: "First artist", Number: 1, LocalizedTitles: []scraper.LocalizedTitle{{Language: "en", Title: "Wrong upstream title"}}},
		{Title: "Imported track", Artist: "First artist", Number: 2},
	})
	revision, err := store.SelectMusicAlbumScrapedFields(ctx, "admin", album.ID, album.Revision, previous, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	phantom := albumRows(t, store, album.ID, "Imported track")[0]
	if _, err := store.SaveMusicTrack(ctx, "admin", phantom.ID, phantom.Revision,
		music.TrackInput{TrackFields: music.TrackFields{Title: "Edited placeholder"}, ArtistNames: []string{"First artist"}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	evidence, err := store.MusicAlbumTrackEvidence(ctx, "admin", album.ID, "local")
	if err != nil || len(evidence) != 2 {
		t.Fatal(evidence, err)
	}
	for _, tc := range []struct {
		title, artist string
		want          bool
	}{
		{"First song", "歌手甲", true},
		{"First song", "Shared alias", false},
		{"First song", "Second artist", false},
		{"Wrong upstream title", "First artist", false},
		{"Edited placeholder", "First artist", false},
	} {
		got := music.MatchesAlbumTracks(evidence, []scraper.Track{{Title: tc.title, Artist: tc.artist}})
		if got != tc.want {
			t.Fatalf("evidence mismatch for %q / %q: %v", tc.title, tc.artist, evidence)
		}
	}
	out, err := store.MusicAlbumTrackEvidence(ctx, "admin", album.ID, "elsewhere")
	if err != nil || len(out) != 0 {
		t.Fatal("provider filter exposed unrelated sources", out, err)
	}
	_, err = store.MusicAlbumTrackEvidence(ctx, "other", album.ID, "")
	matchingError(t, err, "NOT_FOUND")

	before, err := store.MusicAlbumScrapedFields(ctx, "admin", album.ID)
	if err != nil {
		t.Fatal(err)
	}
	key := domain.NewID()
	fields := albumBundleFields([]scraper.Track{{Title: "Edited placeholder", Artist: "First artist"}})
	_, err = store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album.ID, "local", revision, fields, key)
	matchingError(t, err, "ALBUM_TRACK_MISMATCH")
	after, err := store.MusicAlbumScrapedFields(ctx, "admin", album.ID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("mismatch changed the saved release", after, err)
	}
	fields = albumBundleFields([]scraper.Track{{Title: "First song", Artist: "歌手甲"}})
	accepted, err := store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album.ID, "local", revision, fields, key)
	if err != nil || accepted != revision+1 {
		t.Fatal(accepted, err)
	}
	// 已确认的相同提交可在候选过期后重放，不能被再次验证或重复写入。
	for i := range fields {
		fields[i].Item = scraper.Item{}
	}
	replayed, err := store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album.ID, "local", revision, fields, key)
	if err != nil || replayed != accepted {
		t.Fatal("successful guarded selection could not replay", replayed, err)
	}
}

func TestAutoAlbumRejectsChangedSourceBetweenSearchAndSave(t *testing.T) {
	store := visibilityStore(t)
	ctx := t.Context()
	observed := localizedObservation("source", "Observed song", []string{"Observed artist"}, 200000)
	source := visibilityScan(t, store, "admin", "local", observed)[0]
	track, err := store.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := track.Albums[0]
	fields := albumBundleFields([]scraper.Track{{Title: "Observed song", Artist: "Observed artist"}})
	evidence, err := store.MusicAlbumTrackEvidence(ctx, "admin", album.ID, "")
	if err != nil || !music.MatchesAlbumTracks(evidence, fields[1].Item.Tracks) {
		t.Fatal("source did not match before rescan", evidence, err)
	}
	observed.ObservedVersion = "v2"
	observed.Artists = []string{"Corrected artist"}
	visibilityScan(t, store, "admin", "local", observed)
	_, err = store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album.ID, "", album.Revision, fields, domain.NewID())
	matchingError(t, err, "ALBUM_TRACK_MISMATCH")
	if saved, err := store.MusicAlbumScrapedFields(ctx, "admin", album.ID); err != nil || len(saved) != 0 {
		t.Fatal(saved, err)
	}
}

func TestAutoAlbumEvidenceExcludesOtherReleaseOfSameRecording(t *testing.T) {
	store := visibilityStore(t)
	ctx := t.Context()
	one := localizedObservation("first", "Original", []string{"Singer"}, 200000)
	two := localizedObservation("second", "Other release title", []string{"Other credit"}, 200000)
	otherAlbum := "Second album"
	two.Album = &otherAlbum
	sources := visibilityScan(t, store, "admin", "local", one, two)
	track, err := store.MusicTrackForSource(ctx, "admin", sources[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	albumID := track.Albums[0].ID
	if _, err = store.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{sources[0].ID, sources[1].ID}}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	known, err := store.MusicAlbumTrackEvidence(ctx, "admin", albumID, "")
	if err != nil || len(known) != 1 || known[0].Title != "Original" {
		t.Fatal("another release was used to confirm this album", known, err)
	}
}

func TestPostgresAutoAlbumSourceEvidenceAndGuardedSelection(t *testing.T) {
	ctx := t.Context()
	options := testkit.PostgresOptions(t)
	store, err := storage.Open(ctx, "postgres", storage.Options{DataRoot: t.TempDir(), Values: options})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	db := testkit.PostgresDatabase(t, options)
	_, err = db.ExecContext(ctx, `INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document)
 VALUES('observed','admin','local','external','root','song.flac','v1','asset','audio/flac',12,
 '{"title":"Song","artists":["Singer","Guest"],"album":"Album"}')`)
	if err != nil {
		t.Fatal(err)
	}
	var album, artist string
	if err := db.QueryRowContext(ctx, "SELECT id FROM music_albums WHERE name='Album'").Scan(&album); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, "SELECT id FROM music_artists WHERE name='Guest'").Scan(&artist); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, "INSERT INTO music_artist_localized_names(artist_id,language,name) VALUES($1,'zh','嘉宾')", artist)
	if err != nil {
		t.Fatal(err)
	}
	known, err := store.MusicAlbumTrackEvidence(ctx, "admin", album, "local")
	if err != nil || len(known) != 1 || len(known[0].ArtistNames) != 3 {
		t.Fatal(known, err)
	}
	fields := albumBundleFields([]scraper.Track{{Title: "Song", Artist: "Different singer"}})
	_, err = store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album, "", 1, fields, domain.NewID())
	matchingError(t, err, "ALBUM_TRACK_MISMATCH")
	fields = albumBundleFields([]scraper.Track{{Title: "Song", Artist: "嘉宾"}})
	if _, err = store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album, "", 1, fields, domain.NewID()); err != nil {
		t.Fatal(err)
	}
}
