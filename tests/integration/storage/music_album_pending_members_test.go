package storage_test

import (
	"database/sql"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage"
)

func TestMusicAlbumPendingSourcesOnlyReplaceVerifiedPlaceholders(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		ctx := t.Context()
		f := albumImportFixture{t: t, store: store, db: db}
		file := func(id, artist string, ms int64) library.Observation {
			o := releaseFile(id, "Common release", "", "")
			o.Artists, o.DurationMs = []string{artist}, &ms
			return o
		}
		base := file("base", "Singer", 100000)
		f.scan("local", base)
		target := f.album(f.source("local", "base"))
		tracks := []scraper.Track{
			{Title: *base.Title, Artist: "Singer", Duration: 100, Number: 1},
			{Title: "New title", Artist: "Guest", Duration: 190, Number: 2},
			{Title: "Unconfirmed", Artist: "Singer", Duration: 100.1, Number: 3},
			{Title: "Two sources", Artist: "Twins", Duration: 220, Number: 4},
			{Title: "After release identification", Artist: "Late", Duration: 230, Number: 5},
		}
		selectTracks := func(automatic bool) {
			t.Helper()
			album, err := store.MusicAlbum(ctx, "admin", target)
			if err != nil {
				t.Fatal(err)
			}
			fields := albumBundleFields(tracks)
			if automatic {
				_, err = store.SelectAutoMusicAlbumScrapedFields(ctx, "admin", target, "local", album.Revision, fields, domain.NewID())
			} else {
				_, err = store.SelectMusicAlbumScrapedFields(ctx, "admin", target, album.Revision, fields, domain.NewID())
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		selectTracks(false)
		added, ambiguous := file("local-name", "Guest", 190100), file("undecided", "Singer", 100050)
		twinA, twinB := file("twin-a", "Twins", 220000), file("twin-b", "Twins", 220000)
		items := []library.Observation{base, added, ambiguous, twinA, twinB}
		f.scan("local", items...)
		accepted := f.source("local", added.ExternalID)
		staging := f.album(accepted)
		if staging == target {
			t.Fatal("Unnumbered import entered the selected release")
		}
		other := file("other-copy", "Guest", 190100)
		f.scan("other", other)
		otherSource := f.source("other", other.ExternalID)
		otherAlbum := f.album(otherSource)
		if _, err := store.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{accepted, otherSource}}, domain.NewID()); err != nil {
			t.Fatal(err)
		}
		checkSource := func(id, want string) {
			t.Helper()
			var tag, history string
			err := db.QueryRowContext(ctx, `SELECT r.album_id,h.album_id FROM music_album_source_refs r
 JOIN music_album_source_tag_history h ON h.source_id=r.source_id WHERE r.source_id=$1 AND r.origin_id='tag'`, id).Scan(&tag, &history)
			if err != nil || tag != want || history != want {
				t.Fatalf("Source %s has tag/history %s/%s, want %s: %v", id, tag, history, want, err)
			}
		}
		selectTracks(true)
		checkSource(accepted, staging)
		selectTracks(false)
		checkSource(accepted, target)
		checkSource(otherSource, otherAlbum)
		for _, item := range []library.Observation{ambiguous, twinA, twinB} {
			checkSource(f.source("local", item.ExternalID), staging)
		}
		// 已确认原生发行后的人工重选也不能吸收另一个缺少编号的来源。
		f.exec("INSERT INTO music_album_release_identities(owner,namespace,release_id,album_id) VALUES('admin','netease','999',$1)", target)
		late := file("late", "Late", 230000)
		f.scan("local", append(items, late)...)
		selectTracks(false)
		checkSource(f.source("local", late.ExternalID), staging)
	})
}
