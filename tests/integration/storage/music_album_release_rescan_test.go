package storage_test

import (
	"database/sql"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

type curatedSlotAlbum struct {
	file                                                     library.Observation
	albumID, originalSource, originalTrack, overriddenSource string
}

func (f *albumImportFixture) curatedSlot() curatedSlotAlbum {
	t, store, db := f.t, f.store, f.db
	ctx := t.Context()
	const album = "花鳥風月"
	title := "月 feat. ヰ世界情緒"
	file := releaseFile("file", album, "", "")
	file.Title, file.Artists = &title, []string{"Guiano", "ヰ世界情緒"}
	f.scan("original-library", file)
	first := f.source("original-library", "file")
	oldID := f.album(first)
	before, err := store.MusicAlbum(ctx, "admin", oldID)
	if err != nil {
		t.Fatal(err)
	}
	before.SourceLinks = []music.AlbumSourceLink{{PluginID: "musicfs.scraper.bangumi", URL: "https://bgm.tv/subject/439720"}, {PluginID: "musicfs.scraper.itunes", URL: "https://music.apple.com/us/album/1688662053"}}
	locked, err := store.SaveMusicAlbum(ctx, "admin", oldID, before.Revision, before.AlbumFields, "keep-release")
	if err != nil {
		t.Fatal(err)
	}
	revision := locked.Revision
	for _, field := range []works.ScrapedField{
		{Field: "albumDetails", PluginID: "musicfs.scraper.itunes", CandidateToken: "details", Item: scraper.Item{ID: "1688662053", Title: album}},
		{Field: "albumTracks", PluginID: "musicfs.scraper.bangumi", CandidateToken: "tracks", Item: scraper.Item{ID: "439720", Title: album, Tracks: []scraper.Track{{Title: title, Artist: "Guiano / ヰ世界情緒", Disc: 1, Number: 12}}}},
	} {
		revision, err = store.SelectMusicAlbumScrapedField(ctx, "admin", oldID, revision, field, "slot-"+field.Field)
		if err != nil {
			t.Fatal(err)
		}
	}
	original, err := store.MusicTrackForSource(ctx, "admin", first)
	if err != nil {
		t.Fatal(err)
	}
	var bound string
	if err = db.QueryRowContext(ctx, "SELECT track_id FROM music_album_scraped_track_refs WHERE album_id=$1 AND slot='1:12'", oldID).Scan(&bound); err != nil || bound != original.ID {
		t.Fatalf("Selected slot is not occupied by the source: %s, %v", bound, err)
	}
	// 同一个真实演出还可另有独立录音，由逐音源覆盖明确指回原卡。该例外不能反过来为其它专辑认领原生号。
	f.scan("second-library", file)
	second := f.source("second-library", "file")
	f.exec("INSERT INTO music_album_source_tag_overrides(owner,source_id,tag_name,album_id) VALUES('admin',$1,$2,$3)", second, album, oldID)
	f.exec("UPDATE music_album_source_refs SET album_id=$1 WHERE source_id=$2 AND origin_id='tag'", oldID, second)
	file.NetEaseID = "2048604695"
	file.AlbumRelease = &audiotypes.AlbumRelease{Namespace: "netease", ID: "165942825"}
	return curatedSlotAlbum{file: file, albumID: oldID, originalSource: first, originalTrack: original.ID, overriddenSource: second}
}

func TestMusicAlbumNativeReleasePreservesCuratedTrackSlot(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		ctx := t.Context()
		f := albumImportFixture{t: t, store: store, db: db}
		existing := f.curatedSlot()
		f.scan("original-library", existing.file)
		var canonical string
		if err := db.QueryRowContext(ctx, "SELECT album_id FROM music_album_release_identities WHERE owner='admin' AND namespace='netease' AND release_id='165942825'").Scan(&canonical); err != nil || canonical != existing.albumID {
			t.Fatalf("Curated source slot lost its album: got %q, expected %q, %v", canonical, existing.albumID, err)
		}
		if f.album(existing.originalSource) != existing.albumID || f.album(existing.overriddenSource) != existing.albumID {
			t.Fatal("Original members no longer share the curated album")
		}
		fields, err := store.MusicAlbumScrapedFields(ctx, "admin", existing.albumID)
		if err != nil || len(fields) != 2 {
			t.Fatalf("Lost curated metadata: %v, %v", fields, err)
		}
		secondTrack, err := store.MusicTrackForSource(ctx, "admin", existing.overriddenSource)
		if err != nil || secondTrack.ID == existing.originalTrack {
			t.Fatalf("Changed independent recordings: %s, %v", secondTrack.ID, err)
		}
	})
}

func TestMusicAlbumNativeReleaseLateCuratedMember(t *testing.T) {
	checkCuratedMemberAfterNewRelease(t, "")
}

func TestMusicAlbumNativeReleaseRetainsEditedInterimAlbum(t *testing.T) {
	checkCuratedMemberAfterNewRelease(t, "album metadata")
}

func TestMusicAlbumNativeReleaseRetainsManualInterimMember(t *testing.T) {
	checkCuratedMemberAfterNewRelease(t, "manual member")
}

func checkCuratedMemberAfterNewRelease(t *testing.T, protection string) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		f := albumImportFixture{t: t, store: store, db: db}
		ctx := t.Context()
		existing := f.curatedSlot()
		name := *existing.file.Album
		one := releaseFile("fresh-one", name, "8101", existing.file.AlbumRelease.ID)
		two := releaseFile("fresh-two", name, "8102", existing.file.AlbumRelease.ID)
		f.scan("new-library", one, two)
		first, second := f.source("new-library", "fresh-one"), f.source("new-library", "fresh-two")
		interim := f.album(first)
		if interim == existing.albumID || f.album(second) != interim {
			t.Fatal("New source prematurely claimed a cross-platform catalog")
		}
		// 一份文件暂时失去原生编号，但真实来源、标签名、曲名、署名及原有归属均相同。
		withoutID := releaseFile("fresh-two", name, "", "")
		withoutID.ObservedVersion = "v2"
		f.scan("new-library", one, withoutID)
		if f.album(second) != interim {
			t.Fatal("Interim release lost a returning source without its native number")
		}
		var manualTrack string
		const publisher = "Publisher entered by user"
		if protection == "album metadata" {
			card, err := store.MusicAlbum(ctx, "admin", interim)
			if err != nil {
				t.Fatal(err)
			}
			userPublisher := publisher
			card.Publisher = &userPublisher
			if _, err = store.SaveMusicAlbum(ctx, "admin", interim, card.Revision, card.AlbumFields, "edit-interim"); err != nil {
				t.Fatal(err)
			}
		}
		if protection == "manual member" {
			manual, err := store.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: "Manual track"}, AlbumIDs: []string{interim}}, "manual-interim-track")
			if err != nil {
				t.Fatal(err)
			}
			manualTrack = manual.ID
		}
		before := map[string]music.Track{}
		for _, sid := range []string{first, second} {
			track, err := store.MusicTrackForSource(ctx, "admin", sid)
			if err != nil {
				t.Fatal(err)
			}
			before[sid] = track
		}
		// 此时第一次从手工曲位的原文件中读到同一个网易发行号。所有断言都经正式重扫和读取验证。
		f.scan("original-library", existing.file)
		wantNewMembers, wantCards := existing.albumID, 1
		if protection != "" {
			wantNewMembers, wantCards = interim, 2
		}
		checkMembers := func() {
			t.Helper()
			if f.album(existing.originalSource) != existing.albumID || f.album(existing.overriddenSource) != existing.albumID {
				t.Fatal("Late scan moved a member off the established curated album")
			}
			for _, sid := range []string{first, second} {
				track, err := store.MusicTrackForSource(ctx, "admin", sid)
				if err != nil || len(track.AlbumIDs) != 1 || track.AlbumIDs[0] != wantNewMembers || track.ID != before[sid].ID {
					t.Fatalf("Source %q must keep its recording and use album %q: %+v, %v", sid, wantNewMembers, track.AlbumIDs, err)
				}
				if protection == "" && track.Revision <= before[sid].Revision {
					t.Fatalf("Migrated source %q kept a stale recording revision", sid)
				}
				var remembered string
				if err := db.QueryRowContext(ctx, "SELECT album_id FROM music_album_source_tag_history WHERE owner='admin' AND source_id=$1", sid).Scan(&remembered); err != nil || remembered != wantNewMembers {
					t.Fatalf("Rescan history for %q is %q: %v", sid, remembered, err)
				}
			}
		}
		checkMembers()
		f.scan("new-library", one, withoutID)
		f.scan("original-library", existing.file)
		checkMembers()
		if protection == "" {
			var canonical string
			if err := db.QueryRowContext(ctx, "SELECT album_id FROM music_album_release_identities WHERE owner='admin' AND namespace='netease' AND release_id=$1", existing.file.AlbumRelease.ID).Scan(&canonical); err != nil || canonical != existing.albumID {
				t.Fatalf("Future imports would use album %q instead of %q: %v", canonical, existing.albumID, err)
			}
			f.scan("future-library", releaseFile("future", name, "8103", existing.file.AlbumRelease.ID))
			if f.album(f.source("future-library", "future")) != existing.albumID {
				t.Fatal("New instance recreated the duplicate after reconciliation")
			}
		}
		page, err := store.MusicAlbums(ctx, "admin", music.Query{Q: name, Limit: 20})
		if err != nil || len(page.Items) != wantCards {
			t.Fatalf("Expected %d visible albums for %q, got %+v: %v", wantCards, name, page.Items, err)
		}
		var displaced int
		if err := db.QueryRowContext(ctx, "SELECT native_displaced FROM music_albums WHERE id=$1", existing.albumID).Scan(&displaced); err != nil || displaced != 0 {
			t.Fatalf("Retained curated album was marked as displaced: %d, %v", displaced, err)
		}
		if protection == "album metadata" {
			card, err := store.MusicAlbum(ctx, "admin", interim)
			if err != nil || card.Publisher == nil || *card.Publisher != publisher {
				t.Fatalf("Edited interim album changed: %+v, %v", card, err)
			}
		}
		if protection == "manual member" {
			manual, err := store.MusicTrack(ctx, "admin", manualTrack)
			if err != nil || len(manual.AlbumIDs) != 1 || manual.AlbumIDs[0] != interim {
				t.Fatalf("Manual association lost: %+v, %v", manual.AlbumIDs, err)
			}
		}
	})
}

func TestMusicAlbumNativeReleaseReplacedFile(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		f := albumImportFixture{t: t, store: store, db: db}
		byTitle := releaseFile("new-song", "Shared", "61", "600")
		byArtist := releaseFile("new-artist", "Shared", "62", "600")
		partial := releaseFile("same-song", "Shared", "63", "600")
		partial.Artists = []string{"Artist", "Guest"}
		f.scan("files", byTitle, byArtist, partial)
		titleID, artistID, sameID := f.source("files", "new-song"), f.source("files", "new-artist"), f.source("files", "same-song")
		confirmed := f.album(titleID)
		// 同一个路径被同名专辑的新歌替换，而文件不再包含发行号。保留了部分原署名的观测仍有同曲佐证。
		byTitle = releaseFile("new-song", "Shared", "", "")
		otherTitle := "A different song"
		byTitle.Title, byTitle.ObservedVersion = &otherTitle, "v2"
		byArtist = releaseFile("new-artist", "Shared", "", "")
		byArtist.Artists, byArtist.ObservedVersion = []string{"Unrelated band"}, "v2"
		partial = releaseFile("same-song", "Shared", "", "")
		partial.Artists, partial.ObservedVersion = []string{"Guest", "New guest"}, "v2"
		f.scan("files", byTitle, byArtist, partial)
		f.scan("files", byTitle, byArtist, partial)
		if f.album(titleID) == confirmed || f.album(artistID) == confirmed || f.album(sameID) != confirmed {
			t.Fatal("A replacement inherited another song's known release, or the continuing song lost its release")
		}
	})
}

func TestMusicAlbumNativeReleaseRejectsStaleEdits(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		ctx := t.Context()
		f := albumImportFixture{t: t, store: store, db: db}
		changing := releaseFile("track", "Shared", "71", "700")
		stays := releaseFile("stays", "Shared", "72", "701")
		f.scan("files", changing, stays)
		source := f.source("files", "track")
		before, err := store.MusicTrackForSource(ctx, "admin", source)
		if err != nil || len(before.Albums) != 1 {
			t.Fatalf("Initial recording: %+v, %v", before, err)
		}
		old := before.Albums[0]
		newID := f.album(f.source("files", "stays"))
		beforeTarget, err := store.MusicAlbum(ctx, "admin", newID)
		if err != nil {
			t.Fatal(err)
		}
		// 仅发行号变化，曲名、艺人、标签名、文件路径均相同。
		changing = releaseFile("track", "Shared", "71", "701")
		changing.ObservedVersion = "v2"
		f.scan("files", changing, stays)
		after, err := store.MusicTrackForSource(ctx, "admin", source)
		if err != nil || len(after.Albums) != 1 || after.Albums[0].ID != newID || after.Revision <= before.Revision {
			t.Fatalf("Recording did not invalidate a stale edit: %+v, %v", after, err)
		}
		for _, card := range []music.Album{old, beforeTarget} {
			current, err := store.MusicAlbum(ctx, "admin", card.ID)
			if err != nil || current.Revision <= card.Revision {
				t.Fatalf("Changed album membership retained stale revision: %q, %d, %v", card.ID, current.Revision, err)
			}
			_, err = store.SaveMusicAlbum(ctx, "admin", card.ID, card.Revision, card.AlbumFields, "stale-album-"+card.ID)
			matchingError(t, err, "CONFLICT")
			f.scan("files", changing, stays)
			repeated, err := store.MusicAlbum(ctx, "admin", card.ID)
			if err != nil || repeated.Revision != current.Revision {
				t.Fatalf("Identical rescan changed album revision: %q, %d, %v", card.ID, repeated.Revision, err)
			}
		}
		input := music.TrackInput{TrackFields: before.TrackFields, ArtistNames: []string{"Artist"}, AlbumIDs: before.AlbumIDs}
		_, err = store.SaveMusicTrack(ctx, "admin", before.ID, before.Revision, input, "stale-member")
		matchingError(t, err, "CONFLICT")
		repeated, err := store.MusicTrackForSource(ctx, "admin", source)
		if err != nil || repeated.Revision != after.Revision || len(repeated.AlbumIDs) != 1 || repeated.AlbumIDs[0] != newID {
			t.Fatalf("Rescan or stale edit changed the recording: %+v, %v", repeated, err)
		}
	})
}
