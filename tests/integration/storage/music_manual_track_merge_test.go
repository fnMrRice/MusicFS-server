package storage_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

func metadataTrack(t *testing.T, s *sqlite.Store, title string, albums ...string) music.Track {
	t.Helper()
	m, err := s.SaveMusicTrack(t.Context(), "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: title}, AlbumIDs: albums}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func mergeTrackRequest(target music.Track, albumRevision int64, donors ...music.Track) music.MergeTracksInput {
	r := music.MergeTracksInput{TargetTrackID: target.ID, ExpectedAlbumRevision: albumRevision}
	for _, t := range append([]music.Track{target}, donors...) {
		r.Tracks = append(r.Tracks, music.MergeTrackMember{TrackID: t.ID, ExpectedRevision: t.Revision})
	}
	return r
}

func TestManualAlbumTrackMergeRetainsTrackIdentityAndPersonalReferences(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	source := visibilityScan(t, s, "admin", "local", localizedObservation("one", "Local title", []string{"Singer"}, 123456))[0]
	target, err := s.MusicTrackForSource(ctx, "admin", source.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := target.Albums[0]
	extra, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Second album"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	donor := metadataTrack(t, s, "上游曲名", album.ID, extra.ID)
	aliases := []scraper.LocalizedTitle{{Language: "ja", Title: "別名"}}
	donor, err = s.SaveMusicTrack(ctx, "admin", donor.ID, donor.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: donor.Title, LocalizedTitles: aliases}, ArtistNames: []string{"Second artist"}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	// 一个普通歌单可以出现同一首多次；收藏在归并后仍只有一项。
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.DB.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,position) VALUES
 ('admin','fixture','local','entry-a',?,0),('admin','fixture','local','entry-b',?,1),('admin','favorites','',?,?,0),('admin','favorites','',?,?,0)`, donor.ID, target.ID, donor.ID, donor.ID, target.ID, target.ID)
	mustExec(`INSERT INTO music_album_scraped_track_refs(album_id,slot,track_id,plugin_id,release_id,position) VALUES(?,'1:1',?,'fixture','release',0)`, album.ID, donor.ID)
	req := mergeTrackRequest(target, album.Revision, donor)
	out, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, "merge-replay-key")
	if err != nil {
		t.Fatal(err)
	}
	want := scraper.LocalizedTitle{Language: "und", Title: "上游曲名"}
	if out.ID != target.ID || out.Title != target.Title || len(out.Sources) != 1 || out.Sources[0].SourceTrackID != source.ID || !slices.Contains(out.LocalizedTitles, want) || !slices.Contains(out.LocalizedTitles, aliases[0]) || len(out.Albums) != 2 || len(out.Artists) != 2 {
		t.Fatalf("merged result lost metadata: %+v", out)
	}
	replay, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, "merge-replay-key")
	if err != nil || replay.Revision != out.Revision {
		t.Fatalf("idempotent replay failed: %+v %v", replay, err)
	}
	if _, err = s.MusicTrack(ctx, "admin", donor.ID); err == nil {
		t.Fatal("obsolete identity is still visible")
	}
	if got := albumRows(t, s, album.ID, "上游曲名"); len(got) != 1 || got[0].ID != target.ID {
		t.Fatal("merged alias not searchable", got)
	}
	for _, q := range []string{
		"SELECT count(*)=2 FROM music_playlist_entries WHERE playlist_id='fixture' AND track_id=?",
		"SELECT count(*)=1 FROM music_playlist_entries WHERE playlist_id='favorites' AND track_id=?",
		"SELECT count(*)=1 FROM music_album_scraped_track_refs WHERE slot='1:1' AND track_id=?",
	} {
		var ok bool
		if err := s.DB.QueryRowContext(ctx, q, target.ID).Scan(&ok); err != nil || !ok {
			t.Fatal("reference migration failed", q, err)
		}
	}
}

func TestManualAlbumTrackMergeRejectsStaleAndConflictingSelections(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Manual"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	target := metadataTrack(t, s, "Chosen", album.ID)
	donor := metadataTrack(t, s, "Other", album.ID)
	outside := metadataTrack(t, s, "Outside")
	req := mergeTrackRequest(target, album.Revision, donor)
	reject := func(r music.MergeTracksInput, code string) {
		t.Helper()
		_, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, r, domain.NewID())
		var e *domain.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("wanted %s, got %v", code, err)
		}
	}
	reject(mergeTrackRequest(target, album.Revision, donor, outside), "CONFLICT")
	old := req
	old.ExpectedMatchingRevision++
	reject(old, "CONFLICT")
	old = req
	old.ExpectedAlbumRevision++
	reject(old, "CONFLICT")
	staleDonor := donor
	staleDonor.Revision++
	reject(mergeTrackRequest(target, album.Revision, staleDonor), "CONFLICT")
	if _, err = s.MergeMusicAlbumTracks(ctx, "other", album.ID, req, domain.NewID()); err == nil {
		t.Fatal("accepted another user's album")
	}
	out, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, domain.NewID())
	if err != nil || out.ID != target.ID || len(out.Sources) != 0 || len(out.LocalizedTitles) != 1 {
		t.Fatalf("metadata-only merge failed: %+v %v", out, err)
	}
}

func TestManualAlbumTrackMergePersistsPlatformTrackPositions(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Release"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	tracks := []scraper.Track{{Title: "Platform alias", Number: 1, Disc: 1, ExternalID: "old-a"},
		{Title: "Separate song", Number: 2, Disc: 1, ExternalID: "separate"},
		{Title: "Chosen title", Number: 3, Disc: 1, ExternalID: "old-b"}}
	applyAlbumTracks(t, s, album.ID, tracks)
	rows := albumRows(t, s, album.ID, "")
	if len(rows) != 3 {
		t.Fatal("expected 3 platform tracks", rows)
	}
	primary := rows[2]
	out, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, mergeTrackRequest(primary, primary.Albums[0].Revision, rows[0]), domain.NewID())
	if err != nil || out.ID != primary.ID {
		t.Fatal("platform slot merge failed", out, err)
	}
	check := func(second string) {
		t.Helper()
		got := albumRows(t, s, album.ID, "")
		if len(got) != 2 || got[0].Title != "Separate song" || got[1].ID != primary.ID || got[1].Title != primary.Title || !slices.Contains(got[1].LocalizedTitles, scraper.LocalizedTitle{Language: "und", Title: second}) {
			t.Fatalf("lost manually chosen slot or alias: %+v", got)
		}
	}
	check("Platform alias")
	applyAlbumTracks(t, s, album.ID, tracks)
	check("Platform alias")
	// 平台修改主曲位并翻译另一个曲名，稳定编号仍映射到同一身份。
	tracks[1].Number, tracks[2].Number = 1, 2
	tracks[0].Number, tracks[0].Title = 3, "翻译名"
	tracks = []scraper.Track{tracks[1], tracks[2], tracks[0]}
	applyAlbumTracks(t, s, album.ID, tracks)
	check("翻译名")
}
