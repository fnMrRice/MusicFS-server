package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func TestAlbumAutoSaveRechecksNativeRelease(t *testing.T) {
	ctx := t.Context()
	s := visibilityStore(t)
	source := visibilityScan(t, s, "admin", "local", localizedObservation("one", "Shared song", []string{"Singer"}, 120000))[0]
	track, err := s.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil || len(track.Albums) != 1 {
		t.Fatalf("Source albums: %v, %v", track.Albums, err)
	}
	album := track.Albums[0]
	target, err := s.MusicAlbumScrapeTarget(ctx, "admin", album.ID, "")
	if err != nil || target.Release != nil {
		t.Fatalf("Expected unnumbered album: %+v, %v", target, err)
	}
	// 模拟用户预览后才从文件读到原始发行号。原修订仍相同，保存必须重新核验编号。
	if _, err = s.DB.Exec("INSERT INTO music_album_release_identities(owner,namespace,release_id,album_id) VALUES('admin','netease','101',?)", album.ID); err != nil {
		t.Fatal(err)
	}
	target, err = s.MusicAlbumScrapeTarget(ctx, "admin", album.ID, "local")
	if err != nil || target.Release == nil || target.Release.Namespace != "netease" || target.Release.ID != "101" {
		t.Fatalf("Missing source release: %+v, %v", target.Release, err)
	}
	fields := albumBundleFields([]scraper.Track{{Title: "Shared song", Artist: "Singer"}})
	for i := range fields {
		fields[i].PluginID = "musicfs.scraper.netease"
		fields[i].Item.ID = "202"
	}
	_, err = s.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album.ID, "local", album.Revision, fields, domain.NewID())
	matchingError(t, err, "ALBUM_TRACK_MISMATCH")
	stored, err := s.MusicAlbumScrapedFields(ctx, "admin", album.ID)
	if err != nil || len(stored) != 0 {
		t.Fatalf("Rejected edition updated fields: %v, %v", stored, err)
	}
	for i := range fields {
		fields[i].Item.ID = "101"
	}
	if _, err = s.SelectAutoMusicAlbumScrapedFields(ctx, "admin", album.ID, "local", album.Revision, fields, domain.NewID()); err != nil {
		t.Fatal(err)
	}
}
