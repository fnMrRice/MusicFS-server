package storage_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func remoteBinding() playlists.Binding {
	return playlists.Binding{RemoteRef: playlists.RemoteRef{InstanceID: "netease", ExternalID: "123"}, Platform: "netease", ConfigRevision: "config-v1"}
}
func remoteImport(t *testing.T, s *sqlite.Store, owner string, b playlists.Binding) playlists.RemotePlaylist {
	t.Helper()
	p, _, err := s.ImportRemotePlaylist(context.Background(), owner, b, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func remoteClaim(t *testing.T, s *sqlite.Store, p playlists.RemotePlaylist) playlists.SyncJob {
	t.Helper()
	b := playlists.Binding{RemoteRef: p.RemoteRef, Platform: p.Platform, ConfigRevision: p.ConfigRevision}
	_, _, err := s.QueueRemoteSync(context.Background(), p.Owner, p.ID, p.BindingRevision, b, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.ClaimRemoteSync(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return j
}
func remoteObservation(at int64, ids ...string) playlists.Observation {
	count := int64(len(ids))
	o := playlists.Observation{PlaylistObservation: online.PlaylistObservation{Playlist: online.Playlist{ExternalID: "123", Title: "Actual title", TrackCount: &count, SourceURL: "https://music.163.com/playlist?id=123"}, Complete: true, Consistency: online.Unversioned, ObservedAt: time.Unix(at, 0).UTC(), Entries: []online.PlaylistEntry{}}}
	for i, id := range ids {
		o.Entries = append(o.Entries, online.PlaylistEntry{Position: i, ExternalTrackID: id})
	}
	return o
}
func remotePublish(t *testing.T, s *sqlite.Store, p playlists.RemotePlaylist, o playlists.Observation) playlists.Snapshot {
	t.Helper()
	j := remoteClaim(t, s, p)
	snapshot, err := s.FinishRemoteSync(context.Background(), j, o)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestRemotePlaylistConservativeRemovalAndImmutablePages(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := remoteImport(t, s, "owner", remoteBinding())
	first := remotePublish(t, s, p, remoteObservation(1000, "a", "a", "b"))
	if len(first.Entries) != 3 || first.Entries[0].ID == first.Entries[1].ID || first.Entries[0].Source.ID != first.Entries[1].Source.ID {
		t.Fatal("duplicates were collapsed")
	}
	second := remotePublish(t, s, p, remoteObservation(1001, "b", "a"))
	if len(second.Entries) != 3 || second.Entries[0].ID != first.Entries[2].ID || second.Entries[1].ID != first.Entries[0].ID || second.Entries[2].State != "suspectedRemoved" || len(second.Removals) != 0 {
		t.Fatal("first absence deleted or reordered contributions")
	}
	job := remoteClaim(t, s, p)
	incomplete := remoteObservation(1002, "b")
	incomplete.Complete = false
	_, err := s.FinishRemoteSync(ctx, job, incomplete)
	matchingError(t, err, "TEMPORARY_FAILURE")
	if err = s.FailRemoteSync(ctx, job, &domain.Error{Code: "AUTH_EXPIRED", Message: "cookie=secret"}); err != nil {
		t.Fatal(err)
	}
	afterFailure, err := s.RemotePlaylist(ctx, "owner", p.ID)
	if err != nil || afterFailure.Revision != 2 || !afterFailure.Stale || afterFailure.LastSuccessAt == nil || afterFailure.Error.Message == "cookie=secret" {
		t.Fatal("failure changed snapshot or exposed transport detail", err)
	}
	third := remotePublish(t, s, p, remoteObservation(1003, "b", "a"))
	if len(third.Entries) != 2 || len(third.Removals) != 1 || third.Removals[0].EntryID != first.Entries[1].ID || third.Removals[0].FirstMissingObservation != second.ObservationID || third.Removals[0].ConfirmedObservation != third.ObservationID {
		t.Fatal("later independent observation did not confirm occurrence removal")
	}
	old, err := s.RemoteSnapshot(ctx, "owner", p.ID, 1)
	if err != nil || !reflect.DeepEqual(first, old) {
		t.Fatal("old snapshot was mutated", err)
	}
	page, err := playlists.Page(old, 1, 1)
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != first.Entries[1].ID || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatal("fixed revision page changed", err)
	}
	if _, err = s.RemoteSnapshot(ctx, "other", p.ID, 1); err == nil {
		t.Fatal("foreign snapshot accessible")
	}
	if _, err = s.RemoteSource(ctx, "other", first.Entries[0].Source.ID); err == nil {
		t.Fatal("foreign remote source accessible")
	}
	var assets int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM source_assets").Scan(&assets); err != nil || assets != 0 {
		t.Fatal("metadata was turned into audio assets", err)
	}
}

func TestRemotePlaylistObservationReplayAndReappearance(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	p := remoteImport(t, s, "owner", remoteBinding())
	initial := remotePublish(t, s, p, remoteObservation(1000, "a", "b"))
	suspect := remotePublish(t, s, p, remoteObservation(1001, "a"))
	job := remoteClaim(t, s, p)
	_, err := s.FinishRemoteSync(ctx, job, remoteObservation(1001, "a"))
	matchingError(t, err, "CONFLICT")
	if err = s.FailRemoteSync(ctx, job, &domain.Error{Code: "CONFLICT"}); err != nil {
		t.Fatal(err)
	}
	reappeared := remotePublish(t, s, p, remoteObservation(1002, "a", "b"))
	if len(reappeared.Entries) != 2 || reappeared.Entries[1].ID != initial.Entries[1].ID || reappeared.Entries[1].FirstMissingObservation != nil || reappeared.Entries[1].State != "present" {
		t.Fatal("reappearance retained removal suspicion")
	}
	again := remotePublish(t, s, p, remoteObservation(1003, "a"))
	if again.Entries[1].FirstMissingObservation == nil || *again.Entries[1].FirstMissingObservation == suspect.ObservationID {
		t.Fatal("old absence was reused as confirmation")
	}
	current, _ := s.RemotePlaylist(ctx, "owner", p.ID)
	if current.Stale || current.Error != nil {
		t.Fatal("success did not clear stale failure")
	}
}

func TestRemotePlaylistScopeImportIdempotencyAndSharedReferences(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	b := remoteBinding()
	p, merged, err := s.ImportRemotePlaylist(ctx, "owner", b, "import-once-key")
	if err != nil || merged {
		t.Fatal(err)
	}
	duplicate, merged, err := s.ImportRemotePlaylist(ctx, "owner", b, "import-other-key")
	if err != nil || !merged || duplicate.ID != p.ID {
		t.Fatal("same binding imported twice", err)
	}
	otherOwner := remoteImport(t, s, "other-owner", b)
	otherAccount := b
	otherAccount.AccountID = "test-account"
	accountPlaylist := remoteImport(t, s, "owner", otherAccount)
	if p.ID == otherOwner.ID || p.ID == accountPlaylist.ID {
		t.Fatal("owner/account scopes collapsed")
	}
	changed := b
	changed.ExternalID = "456"
	wrongPlatform := changed
	wrongPlatform.Platform = "qqmusic"
	_, _, err = s.ImportRemotePlaylist(ctx, "owner", wrongPlatform, "wrong-platform-key")
	matchingError(t, err, "CONFLICT")
	_, _, err = s.ImportRemotePlaylist(ctx, "owner", changed, "import-once-key")
	matchingError(t, err, "CONFLICT")
	first := remotePublish(t, s, p, remoteObservation(1000, "a"))
	p2 := remoteImport(t, s, "owner", changed)
	observation := remoteObservation(1001, "a")
	observation.Playlist.ExternalID = "456"
	second := remotePublish(t, s, p2, observation)
	if first.Entries[0].Source.ID != second.Entries[0].Source.ID {
		t.Fatal("remote source identity changed by playlist")
	}
	if first.Entries[0].Source.PlaybackAvailability != "unsupported" {
		t.Fatal("metadata claimed playback")
	}
	list, err := s.RemotePlaylists(ctx, "other-owner", "", 101)
	if err != nil || len(list) != 1 || list[0].ID != otherOwner.ID {
		t.Fatal("playlist list leaked another owner", err)
	}
}
