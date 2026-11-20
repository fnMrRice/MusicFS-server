package storage_test

import (
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

const bangumiPlugin = "musicfs.scraper.bangumi"

func mergeBangumiChapters(t *testing.T, s *sqlite.Store, albumID string, tracks []scraper.Track) {
	t.Helper()
	fields := albumBundleFields(tracks)
	for i := range fields {
		fields[i].PluginID = bangumiPlugin
		fields[i].Item.Artist = ""
	}
	current, err := s.MusicAlbum(t.Context(), "admin", albumID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MergeMusicAlbumMetadata(t.Context(), "admin", albumID, current.Revision, fields, []string{"tracks"}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
}

func TestBangumiAlbumRefreshCorrectsOnlyProvenScrapedPlaceholders(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	source := visibilityScan(t, s, "admin", "local", localizedObservation("local", "Local / Person", []string{"Local artist"}, 120000))[0]
	local, err := s.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := local.Albums[0]
	oldAlias := scraper.LocalizedTitle{Language: "zh", Title: "曲名 / 作者"}
	newAlias := scraper.LocalizedTitle{Language: "zh", Title: "曲名"}
	old := []scraper.Track{
		{Title: "Song / Producer feat. Vocal", LocalizedTitles: []scraper.LocalizedTitle{oldAlias}, ExternalID: "1", Number: 1},
		{Title: "Edited / Singer", ExternalID: "2", Number: 2},
		{Title: "Local / Person", ExternalID: "3", Number: 3},
		{Title: "Merged / Singer", ExternalID: "4", Number: 4},
		{Title: "Without ID / Singer", Number: 5},
		{Title: "Another / Singer", ExternalID: "6", Number: 6},
	}
	mergeBangumiChapters(t, s, album.ID, old)
	before := albumRows(t, s, album.ID, "")
	if len(before) != len(old) || before[2].ID != local.ID {
		t.Fatal("unexpected initial album", before)
	}
	if _, err := s.SaveMusicTrack(ctx, "admin", before[1].ID, before[1].Revision, music.TrackInput{
		TrackFields: music.TrackFields{Title: before[1].Title}, ArtistNames: []string{"My singer"}, AlbumIDs: []string{album.ID},
	}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO music_album_merged_slots(album_id,plugin_id,release_id,slot_key,track_id,preferred)
 VALUES(?,?,'release','id:4',?,1)`, album.ID, bangumiPlugin, before[3].ID); err != nil {
		t.Fatal(err)
	}
	updated := []scraper.Track{
		{Title: "Song", Artist: "Vocal", LocalizedTitles: []scraper.LocalizedTitle{newAlias}, ExternalID: "1", Number: 1},
		{Title: "Edited", Artist: "Singer", ExternalID: "2", Number: 2},
		{Title: "Local", Artist: "Person", ExternalID: "3", Number: 3},
		{Title: "Merged", Artist: "Singer", ExternalID: "4", Number: 4},
		{Title: "Without ID", Artist: "Singer", Number: 5},
		{Title: "Another", ExternalID: "6", Number: 6},
	}
	mergeBangumiChapters(t, s, album.ID, updated)
	after := albumRows(t, s, album.ID, "")
	if len(after) != len(before) {
		t.Fatal("duplicate or missing tracks after refresh", after)
	}
	for i := range after {
		if after[i].ID != before[i].ID {
			t.Fatalf("track %d changed identity", i)
		}
	}
	if first := after[0]; first.Title != "Song" || len(first.Artists) != 1 || first.Artists[0].Name != "Vocal" ||
		!slices.Equal(first.LocalizedTitles, []scraper.LocalizedTitle{newAlias}) {
		t.Fatal("incorrect corrected chapter", first)
	}
	if after[1].Title != old[1].Title || len(after[1].Artists) != 1 || after[1].Artists[0].Name != "My singer" ||
		after[2].Title != local.Title || len(after[2].Sources) != 1 || after[2].Artists[0].Name != "Local artist" ||
		after[3].Title != old[3].Title || after[4].Title != old[4].Title {
		t.Fatal("refresh overwrote protected metadata", after)
	}
	if after[5].Title != "Another" || len(after[5].Artists) != 0 {
		t.Fatal("chapter without a vocalist was not corrected", after[5])
	}
	mergeBangumiChapters(t, s, album.ID, updated)
	again := albumRows(t, s, album.ID, "")
	if again[0].Revision != after[0].Revision || again[5].Revision != after[5].Revision {
		t.Fatal("reapplying unchanged credits adjusted the revision")
	}
}

func TestBangumiAlbumRefreshKeepsThreeRecordingsOfTheSameTitle(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	source := visibilityScan(t, s, "admin", "local", localizedObservation("local-bird", "鳥", []string{"Local singer"}, 200000))[0]
	local, err := s.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	albumID := local.Albums[0].ID
	// 《花鳥風月》的第 2、6、10 首在 Bangumi 各有章节编号；第 2 首的章节没有署名。
	old := []scraper.Track{
		{Title: "鳥", Disc: 1, Number: 2, ExternalID: "1217060"},
		{Title: "鳥 - 幸祜 &amp; 春猿火", Disc: 1, Number: 6, ExternalID: "1217064"},
		{Title: "鳥 - Guiano", Disc: 1, Number: 10, ExternalID: "1217068"},
	}
	mergeBangumiChapters(t, s, albumID, old)
	before := albumRows(t, s, albumID, "")
	if len(before) != 3 || before[0].ID != local.ID || before[1].Title != old[1].Title || before[2].Title != old[2].Title {
		t.Fatal("initial chapters did not bind to distinct tracks", before)
	}
	corrected := slices.Clone(old)
	corrected[1].Title, corrected[1].Artist = "鳥", "幸祜 & 春猿火"
	corrected[2].Title, corrected[2].Artist = "鳥", "Guiano"
	artists := []string{"Local singer", corrected[1].Artist, corrected[2].Artist}
	for range 2 {
		mergeBangumiChapters(t, s, albumID, corrected)
		current := albumRows(t, s, albumID, "鳥")
		if len(current) != len(corrected) {
			t.Fatal("same-title recordings were collapsed or duplicated", current)
		}
		for i, track := range current {
			if track.ID != before[i].ID || track.Title != "鳥" || len(track.Artists) != 1 || track.Artists[0].Name != artists[i] ||
				i == 0 && (len(track.Sources) != 1 || track.Sources[0].SourceTrackID != source.ID) || i > 0 && len(track.Sources) != 0 {
				t.Fatalf("chapter %d lost its performer, position or source: %+v", corrected[i].Number, track)
			}
		}
		fields, err := s.MusicAlbumScrapedFields(ctx, "admin", albumID)
		if err != nil || len(fields) != 2 || fields[1].Field != "albumTracks" || len(fields[1].Item.Tracks) != len(corrected) {
			t.Fatal("unexpected saved platform track list", fields, err)
		}
		for i, chapter := range fields[1].Item.Tracks {
			want := corrected[i]
			if chapter.ExternalID != want.ExternalID || chapter.Disc != want.Disc || chapter.Number != want.Number ||
				chapter.Title != want.Title || chapter.Artist != want.Artist {
				t.Fatal("incorrect saved platform chapter", chapter)
			}
		}
	}
}
