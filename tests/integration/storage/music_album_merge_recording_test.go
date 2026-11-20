package storage_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestManualAlbumTrackMergeMultipleSourceGroups(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	src := visibilityScan(t, s, "admin", "local",
		localizedObservation("one", "Preferred", []string{"A"}, 100000),
		localizedObservation("two", "Alternate", []string{"A"}, 100001),
		localizedObservation("three", "Third", []string{"B"}, 100002),
		localizedObservation("four", "Fourth", []string{"B"}, 100003))
	get := func(i int) music.Track {
		t.Helper()
		r, err := s.MusicTrackForSource(ctx, "admin", src[i].ID)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	primary, other := get(0), get(1)
	album := primary.Albums[0]
	placeholder := metadataTrack(t, s, "Translated", album.ID)
	if _, err := s.DB.ExecContext(ctx, "INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position) VALUES('admin','list','binding','entry',?,?,0)", other.ID, other.Sources[0].SourceTrackID); err != nil {
		t.Fatal(err)
	}
	req := mergeTrackRequest(primary, album.Revision, other, placeholder)
	out, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, "multi-merge-first")
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == primary.ID || len(out.Sources) != 2 || out.Title != primary.Title || !slices.Contains(out.LocalizedTitles, scraper.LocalizedTitle{Language: "und", Title: "Translated"}) || out.SelectedSource.ManualSourceTrackID == nil || *out.SelectedSource.ManualSourceTrackID != primary.Sources[0].SourceTrackID {
		t.Fatalf("lost primary or versions: %+v", out)
	}
	if get(0).ID != out.ID || get(1).ID != out.ID {
		t.Fatal("source identities were not regrouped")
	}
	var entry, forced string
	if err := s.DB.QueryRowContext(ctx, "SELECT track_id,forced_source_id FROM music_playlist_entries WHERE entry_id='entry'").Scan(&entry, &forced); err != nil || entry != out.ID || forced != src[1].ID {
		t.Fatal("lost playlist or selected version", entry, forced, err)
	}
	replay, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, "multi-merge-first")
	if err != nil || replay.ID != out.ID || replay.Revision != out.Revision {
		t.Fatal("multi-source replay failed", replay, err)
	}
	group, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{src[2].ID, src[3].ID}, ExpectedRevision: 1}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	previous := get(2)
	if len(previous.Sources) != 2 || group.RecordingID == nil || previous.ID != *group.RecordingID {
		t.Fatal("expected two recording groups")
	}
	if _, err = s.DB.ExecContext(ctx, "INSERT INTO recording_cover_preferences(owner,recording_id,revision,object_id) VALUES('admin',?,1,'chosen-art')", previous.ID); err != nil {
		t.Fatal(err)
	}
	req = mergeTrackRequest(get(0), out.Albums[0].Revision, previous)
	req.ExpectedMatchingRevision = group.Revision
	out2, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, domain.NewID())
	if err != nil || out2.ID != out.ID || out2.Title != primary.Title || len(out2.Sources) != 4 || get(3).ID != out.ID {
		t.Fatalf("lost existing recording group: %+v %v", out2, err)
	}
	var artwork string
	if err = s.DB.QueryRowContext(ctx, "SELECT object_id FROM recording_cover_preferences WHERE owner='admin' AND recording_id=?", out2.ID).Scan(&artwork); err != nil || artwork != "chosen-art" {
		t.Fatal("lost recording cover preference", artwork, err)
	}
	members := albumRows(t, s, album.ID, "")
	if len(members) != 1 || members[0].ID != out.ID {
		t.Fatalf("obsolete members: %+v", members)
	}
	if _, err = s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: group.Revision + 1, SourceTrackID: src[0].ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	if get(0).ID != src[0].ID || get(0).Title != primary.Title || len(get(1).Sources) != 3 {
		t.Fatal("source identities did not survive subsequent unlink")
	}
}

func TestManualAlbumTrackMergeWritesEditedMetadataToNewRecording(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	first := visibilityScan(t, s, "admin", "local", localizedObservation("a", "First", []string{"First artist"}, 120000))[0]
	second := visibilityScan(t, s, "admin", "other-provider", localizedObservation("b", "Second", []string{"Second artist"}, 120001))[0]
	a, err := s.MusicTrackForSource(ctx, "admin", first.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.MusicTrackForSource(ctx, "admin", second.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := a.Albums[0]
	artist := b.Artists[0]
	b, err = s.SaveMusicTrack(ctx, "admin", b.ID, b.Revision, music.TrackInput{TrackFields: b.TrackFields,
		ArtistNames: []string{artist.Name}, AlbumIDs: []string{album.ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	localized := []scraper.LocalizedTitle{{Language: "ja", Title: "別名"}}
	year, rating := 2024, 4
	req := mergeTrackRequest(a, album.Revision, b)
	req.Metadata = &music.MergeTrackMetadata{
		TrackFields: music.TrackFields{Title: " Revised ", LocalizedTitles: localized, Year: &year, Rating: &rating},
		ArtistNames: []string{artist.Name, "New artist"},
	}
	merged, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if merged.ID == a.ID || merged.ID == b.ID || len(merged.Sources) != 2 || len(merged.Albums) != len(b.Albums) {
		t.Fatal("merge lost recording identity, sources or membership", merged)
	}
	// 合并前打开的编辑窗口仍持有原单文件身份，此时继续保存必须提示重新读取。
	_, err = s.SaveMusicTrack(ctx, "admin", b.ID, b.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: "Stale edit"}}, domain.NewID())
	var stale *domain.Error
	if !errors.As(err, &stale) || stale.Code != "RESOURCE_CHANGED" {
		t.Fatalf("stale editor wrote a retired source identity: %v", err)
	}
	if _, err = s.MusicTrack(ctx, "admin", b.ID); !errors.As(err, &stale) || stale.Code != "RESOURCE_CHANGED" {
		t.Fatalf("stale track ID still read as editable: %v", err)
	}
	current, err := s.MusicTrackForSource(ctx, "admin", second.ID)
	if err != nil || current.ID != merged.ID || current.Title != merged.Title {
		t.Fatalf("source did not resolve to the edited recording: %+v %v", current, err)
	}
	found, err := s.MusicTracks(ctx, "admin", music.Query{AlbumID: album.ID, Q: "別名", ProviderID: "local", Limit: 20})
	if err != nil || len(found.Items) != 1 || len(found.Items[0].Sources) != 2 {
		t.Fatal("provider-filtered search did not retain the edited recording and both versions", found, err)
	}
	result := found.Items[0]
	if result.Title != "Revised" || result.Year == nil || *result.Year != year || result.Rating == nil || *result.Rating != rating ||
		result.Genre != nil || !slices.Equal(result.LocalizedTitles, localized) || len(result.Artists) != 2 ||
		result.Artists[0].ID != artist.ID || result.Artists[1].Name != "New artist" {
		t.Fatal("stored metadata differs from the edited values", result)
	}
	if _, err = s.SetFavoriteSource(ctx, "admin", result.ID, &second.ID); err != nil {
		t.Fatal(err)
	}
	favorites, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 10)
	if err != nil || len(favorites.Items) != 1 {
		t.Fatalf("could not read favorited recording: %+v %v", favorites, err)
	}
	entry := favorites.Items[0]
	if entry.Track == nil || entry.Track.ID != second.ID || entry.MusicTrack.Title != result.Title || !slices.Equal(entry.MusicTrack.LocalizedTitles, localized) {
		t.Fatal("favorite lost edited metadata or the pinned source", entry)
	}
}

func TestManualAlbumTrackMergeSourceConflictRollsBack(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	src := visibilityScan(t, s, "admin", "local", localizedObservation("a", "First", []string{"Singer"}, 120000), localizedObservation("b", "Second", []string{"Singer"}, 120001))
	a, err := s.MusicTrackForSource(ctx, "admin", src[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.MusicTrackForSource(ctx, "admin", src[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	album := a.Albums[0]
	d := metadataTrack(t, s, "Placeholder", album.ID)
	req := mergeTrackRequest(a, album.Revision, d, b)
	req.Metadata = &music.MergeTrackMetadata{TrackFields: music.TrackFields{Title: "Updated", LocalizedTitles: []scraper.LocalizedTitle{}}, ArtistNames: []string{"New artist"}}
	assertReject := func(code string) {
		t.Helper()
		_, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, domain.NewID())
		var e *domain.Error
		if !errors.As(err, &e) || e.Code != code {
			t.Fatalf("wanted %s, got %v", code, err)
		}
		rows := albumRows(t, s, album.ID, "")
		if len(rows) != 3 || rows[0].Albums[0].Revision != album.Revision {
			t.Fatal("failed merge changed the album", rows)
		}
		for _, track := range rows {
			if track.Title == "Updated" {
				t.Fatal("failed merge published edited metadata")
			}
		}
		var moved int
		if err = s.DB.QueryRowContext(ctx, "SELECT count(*) FROM recording_source_links WHERE recording_id IS NOT NULL").Scan(&moved); err != nil || moved != 0 {
			t.Fatal("failed merge changed sources", moved, err)
		}
	}
	exclusion := matching.ExclusionRequest{LeftSourceTrackID: src[0].ID, RightSourceTrackID: src[1].ID, Excluded: true}
	set, err := s.SetExclusion(ctx, "admin", exclusion, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	req.ExpectedMatchingRevision = set.Revision
	assertReject("CONFLICT")
	exclusion.ExpectedRevision, exclusion.Excluded = set.Revision, false
	unset, err := s.SetExclusion(ctx, "admin", exclusion, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	req.ExpectedMatchingRevision = unset.Revision
	extra, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Other release"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, "INSERT INTO music_manual_album_tracks(album_id,track_id,position) VALUES(?,?,0),(?,?,1)", extra.ID, a.ID, extra.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.ExecContext(ctx, `INSERT INTO music_album_scraped_track_refs(album_id,slot,track_id,plugin_id,release_id,position) VALUES
 (?,'1:1',?,'p','r',0),(?,'1:2',?,'p','r',1)`, extra.ID, a.ID, extra.ID, b.ID); err != nil {
		t.Fatal(err)
	}
	assertReject("ALBUM_TRACK_SLOT_CONFLICT")
}

func TestManualAlbumTrackMergeUsesScrapedPrimary(t *testing.T) {
	s := visibilityStore(t)
	ctx := t.Context()
	src := visibilityScan(t, s, "admin", "local", localizedObservation("a", "Local filename", []string{"A"}, 100001))[0]
	local, err := s.MusicTrackForSource(ctx, "admin", src.ID)
	if err != nil {
		t.Fatal(err)
	}
	album := local.Albums[0]
	tracks := []scraper.Track{{Title: "平台原名", Artist: "B", Disc: 1, Number: 1, ExternalID: "platform-1"}}
	applyAlbumTracks(t, s, album.ID, tracks)
	grey := albumRows(t, s, album.ID, "平台原名")[0]
	if len(grey.Sources) != 0 {
		t.Fatal("expected metadata-only track")
	}
	out, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, mergeTrackRequest(grey, grey.Albums[0].Revision, local), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != grey.ID || out.Title != "平台原名" || out.SelectedSource == nil || out.SelectedSource.ID != src.ID || len(out.Sources) != 1 || !slices.Contains(out.LocalizedTitles, scraper.LocalizedTitle{Language: "und", Title: "Local filename"}) {
		t.Fatalf("failed to keep the requested primary: %+v", out)
	}
	applyAlbumTracks(t, s, album.ID, tracks)
	if rows := albumRows(t, s, album.ID, ""); len(rows) != 1 || rows[0].ID != grey.ID {
		t.Fatalf("refresh changed primary: %+v", rows)
	}
	// 源文件保留供解关联。即使旧关系仍引用它，也不能把已折叠的旧行选为新主曲。
	if _, err = s.DB.ExecContext(ctx, "INSERT INTO music_manual_album_tracks(album_id,track_id,position) VALUES(?,?,9)", album.ID, src.ID); err != nil {
		t.Fatal(err)
	}
	req := mergeTrackRequest(local, albumRows(t, s, album.ID, "")[0].Albums[0].Revision, out)
	req.ExpectedMatchingRevision = 1
	if _, err = s.MergeMusicAlbumTracks(ctx, "admin", album.ID, req, domain.NewID()); err == nil {
		t.Fatal("accepted a retired source-track identity")
	}
}
