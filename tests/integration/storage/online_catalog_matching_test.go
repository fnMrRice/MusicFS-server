package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func catalogOnline(id, title string) online.Track {
	duration := int64(201107)
	return online.Track{ExternalID: id, Title: title, Album: "Original album", Artists: []string{"Artist"}, DurationMs: &duration}
}

func catalogLocal(id, title string) library.Observation {
	track := evidenceObservation(id, "v1")
	o := catalogOnline("", title)
	duration := int64(201000)
	track.Title, track.Album, track.Artists, track.DurationMs = &o.Title, &o.Album, o.Artists, &duration
	return track
}

func catalogStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s := evidenceStore(t)
	if err := s.BindMusicSourcePlugins(t.Context(), map[string]string{"netease": pluginproto.OnlinePluginID(online.Netease)}); err != nil {
		t.Fatal(err)
	}
	return s
}

func catalogPublish(t *testing.T, s *sqlite.Store, metadata online.Track) library.Track {
	t.Helper()
	source, err := s.PublishOnlineTrack(t.Context(), "admin", "netease", metadata)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func expectCatalogGroup(t *testing.T, s *sqlite.Store, id, target string) {
	t.Helper()
	link, err := s.GetLink(t.Context(), "admin", id)
	if err != nil || (target == "" && link.RecordingID != nil) || (target != "" && (link.RecordingID == nil || *link.RecordingID != target)) {
		t.Fatalf("unexpected recording for %s: %+v, expected %q, err=%v", id, link, target, err)
	}
}

func TestNewNeteaseFavoriteAttachesToExistingRecording(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	local := publishEvidenceTracks(t, s, catalogLocal("file-a", "Song"), catalogLocal("file-b", "Song"))
	group, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{local[0].ID, local[1].ID}}, "catalog-local-recording")
	if err != nil {
		t.Fatal(err)
	}
	id := *group.RecordingID
	if _, err = s.SetDefaultSource(ctx, "admin", matching.DefaultSourceRequest{ExpectedRevision: group.Revision, RecordingID: id, SourceTrackID: &local[0].ID}, "catalog-keep-default"); err != nil {
		t.Fatal(err)
	}
	b := remoteBinding()
	b.AccountID = "account"
	link, err := s.LinkPlaylist(ctx, "admin", b, personal.Favorites)
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRemoteSync(ctx)
	if err != nil || job.ID != link.JobID {
		t.Fatal("missing import", err)
	}
	o := remoteObservation(1000, "1234", "1234")
	metadata := catalogOnline("1234", "Song")
	o.Tracks[0].Track = &metadata
	if _, err = s.FinishRemoteSync(ctx, job, o); err != nil {
		t.Fatal(err)
	}
	track, err := s.MusicTrack(ctx, "admin", id)
	if err != nil || len(track.Sources) != 3 || track.SelectedSource == nil || track.SelectedSource.ID != local[0].ID {
		t.Fatalf("lost local recording or preferred source: %+v %v", track, err)
	}
	favorites, err := s.Playlist(ctx, "admin", personal.Favorites)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.PlaylistSnapshot(ctx, "admin", personal.Favorites, favorites.SnapshotRevision)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Source.TrackID == nil || *snapshot.Entries[0].Source.TrackID != id {
		t.Fatalf("favorite does not use canonical recording: %+v %v", snapshot, err)
	}
	remote := catalogPublish(t, s, metadata)
	expectCatalogGroup(t, s, remote.ID, id)
	if rev, err := s.MatchingRevision(ctx, "admin"); err != nil || rev != 3 {
		t.Fatal("repeat import advanced matching revision", rev, err)
	}
}

func TestExistingNeteaseSourcesRemainSeparateOnRescanResolveAndSync(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	a, b := catalogOnline("101", "First"), catalogOnline("202", "Second")
	first, second := catalogPublish(t, s, a), catalogPublish(t, s, b)
	if _, err := s.PutFavorite(ctx, "admin", first.ID); err != nil {
		t.Fatal(err)
	}
	files := []library.Observation{catalogLocal("file-first", "First"), catalogLocal("file-second", "Second")}
	files[0].NetEaseID, files[1].NetEaseID = a.ExternalID, b.ExternalID
	files[0].ProbeStatus, files[1].ProbeStatus = "succeeded", "succeeded"
	publishEvidenceObservations(t, s, files...)
	catalogPublish(t, s, a)
	binding := remoteBinding()
	p := remoteImport(t, s, "admin", binding)
	o := remoteObservation(1000, b.ExternalID)
	o.Tracks[0].Track = &b
	remotePublish(t, s, p, o)
	expectCatalogGroup(t, s, first.ID, "")
	expectCatalogGroup(t, s, second.ID, "")
	if rev, err := s.MatchingRevision(ctx, "admin"); rev != 0 || err != nil {
		t.Fatal("existing sources regrouped", rev, err)
	}
	fav, err := s.PutFavorite(ctx, "admin", first.ID)
	if err != nil || fav.MusicTrack.ID != first.ID {
		t.Fatal("old favorite changed", err)
	}
}

func TestNewNeteaseSourceHonorsLocalIdentityAmbiguityAndManualUnlink(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	files := []library.Observation{catalogLocal("id-file", "Local displayed title"), catalogLocal("a", "Repeated"), catalogLocal("b", "Repeated"), catalogLocal("unlinked", "Unlinked")}
	files[0].NetEaseID = "321"
	files[0].ProbeStatus = "succeeded"
	tracks := publishEvidenceTracks(t, s, files...)
	for _, track := range tracks {
		if track.DisplayName == "unlinked" {
			if _, err := s.Unlink(ctx, "admin", matching.UnlinkRequest{SourceTrackID: track.ID}, "catalog-explicit-unlink"); err != nil {
				t.Fatal(err)
			}
		}
	}
	match := catalogPublish(t, s, catalogOnline("321", "Different provider title"))
	link, err := s.GetLink(ctx, "admin", match.ID)
	if err != nil || link.RecordingID == nil {
		t.Fatal("current embedded identity did not match", err)
	}
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("322", "Local displayed title")).ID, "")
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("323", "Repeated")).ID, "")
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("324", "Unlinked")).ID, "")
}

func TestNewNeteaseBatchWithTwoPlatformIDsLeavesSongSeparate(t *testing.T) {
	s := catalogStore(t)
	local := publishEvidenceTracks(t, s, catalogLocal("file", "Repeated"))[0]
	p := remoteImport(t, s, "admin", remoteBinding())
	o := remoteObservation(1000, "101", "202")
	for i := range o.Tracks {
		metadata := catalogOnline(o.Tracks[i].ExternalID, "Repeated")
		o.Tracks[i].Track = &metadata
	}
	remotePublish(t, s, p, o)
	expectCatalogGroup(t, s, local.ID, "")
	for _, i := range o.Tracks {
		expectCatalogGroup(t, s, catalogPublish(t, s, *i.Track).ID, "")
	}
	if rev, err := s.MatchingRevision(t.Context(), "admin"); rev != 0 || err != nil {
		t.Fatal("ambiguous batch mutated recordings", rev, err)
	}
}

func TestNewNeteaseSourcePreservesExistingFavoriteAndPlaylistChoice(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	file := publishEvidenceTracks(t, s, catalogLocal("original-file", "Song"))[0]
	if _, err := s.PutFavorite(ctx, "admin", file.ID); err != nil {
		t.Fatal(err)
	}
	before, err := s.SetFavoriteSource(ctx, "admin", file.ID, &file.ID)
	if err != nil {
		t.Fatal(err)
	}
	p := playlistCreate(t, s, "admin", "Local", playlists.PlaylistBinding{Kind: "local", Entries: []playlists.LocalEntry{{Source: playlists.SourceRef{TrackID: &file.ID, ForcedSourceID: &file.ID}}}})
	b := remoteBinding()
	b.AccountID = "account"
	if _, err := s.LinkPlaylist(ctx, "admin", b, p.ID); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := remoteObservation(1000, "501")
	metadata := catalogOnline("501", "Song")
	o.Tracks[0].Track = &metadata
	if _, err := s.FinishRemoteSync(ctx, job, o); err != nil {
		t.Fatal(err)
	}
	link, err := s.GetLink(ctx, "admin", file.ID)
	if err != nil || link.RecordingID == nil {
		t.Fatal("standalone file did not gain its source", err)
	}
	id := *link.RecordingID
	favorites, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 10)
	if err != nil || len(favorites.Items) != 1 {
		t.Fatal(favorites, err)
	}
	fav := favorites.Items[0]
	if fav.MusicTrack.ID != id || fav.Track == nil || fav.Track.ID != file.ID || !fav.RecordedAt.Equal(before.RecordedAt) {
		t.Fatal("favorite lost its choice or timestamp", fav)
	}
	playlist, err := s.Playlist(ctx, "admin", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := s.PlaylistSnapshot(ctx, "admin", p.ID, playlist.SnapshotRevision)
	if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Source.TrackID == nil || *snapshot.Entries[0].Source.TrackID != id ||
		snapshot.Entries[0].Source.ForcedSourceID == nil || *snapshot.Entries[0].Source.ForcedSourceID != file.ID {
		t.Fatalf("linked playlist duplicated or lost its file: %+v %v", snapshot, err)
	}
}

func TestNewNeteaseSourceRespectsRecordedExclusionAndLockedSingleton(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	other, err := s.PublishOnlineTrack(ctx, "admin", "other-cloud", catalogOnline("610", "Originally separate"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BindMusicSourcePlugins(ctx, map[string]string{"other-cloud": pluginproto.OnlinePluginID(online.Netease)}); err != nil {
		t.Fatal(err)
	}
	files := publishEvidenceTracks(t, s, catalogLocal("file", "Song"), catalogLocal("curated", "Locked"))
	for _, f := range files {
		if f.DisplayName == "file" {
			if _, err := s.SetExclusion(ctx, "admin", matching.ExclusionRequest{LeftSourceTrackID: f.ID, RightSourceTrackID: other.ID, Excluded: true}, "catalog-rejected-pair"); err != nil {
				t.Fatal(err)
			}
		} else {
			current, err := s.MusicTrack(ctx, "admin", f.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.SaveMusicTrack(ctx, "admin", f.ID, current.Revision, music.TrackInput{TrackFields: music.TrackFields{Title: "Curated title"}}, "catalog-curated-track"); err != nil {
				t.Fatal(err)
			}
		}
	}
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("610", "Song")).ID, "")
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("620", "Locked")).ID, "")
}

func TestNewNeteaseSourceRejectsConflictingFileEvidenceAcrossRecording(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	files := []library.Observation{catalogLocal("clear", "Song"), catalogLocal("conflict", "Song"), catalogLocal("alone", "Standalone")}
	for _, i := range []int{1, 2} {
		files[i].NetEaseIDConflict = true
		files[i].ProbeStatus = "succeeded"
	}
	tracks := publishEvidenceTracks(t, s, files...)
	var groupIDs []string
	for _, f := range tracks {
		if f.DisplayName != "alone" {
			groupIDs = append(groupIDs, f.ID)
		}
	}
	group, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: groupIDs}, "catalog-group-conflicting-file")
	if err != nil {
		t.Fatal(err)
	}
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("840", "Song")).ID, "")
	expectCatalogGroup(t, s, catalogPublish(t, s, catalogOnline("841", "Standalone")).ID, "")
	unchanged, err := s.MusicTrack(ctx, "admin", *group.RecordingID)
	if err != nil || len(unchanged.Sources) != 2 {
		t.Fatal("conflicting file allowed an indirect recording match", err)
	}
}
