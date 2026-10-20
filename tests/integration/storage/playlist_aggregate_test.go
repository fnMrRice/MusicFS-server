package storage_test

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func playlistString(s string) *string { return &s }
func localBinding(ids ...string) playlists.PlaylistBinding {
	b := playlists.PlaylistBinding{Kind: "local", Entries: []playlists.LocalEntry{}}
	for _, id := range ids {
		b.Entries = append(b.Entries, playlists.LocalEntry{Source: playlists.SourceRef{SourceTrackID: playlistString(id)}})
	}
	return b
}
func playlistCreate(t *testing.T, s *sqlite.Store, owner, name string, bindings ...playlists.PlaylistBinding) playlists.Playlist {
	t.Helper()
	p, err := s.CreatePlaylist(context.Background(), owner, playlists.CreatePlaylistRequest{Name: name, Bindings: bindings}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func playlistBuild(t *testing.T, s *sqlite.Store, owner string, p playlists.Playlist) playlists.AggregateSnapshot {
	t.Helper()
	snapshot, err := s.RebuildPlaylist(context.Background(), owner, p.ID, playlists.RebuildRequest{ExpectedRuleRevision: p.RuleRevision}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func playlistPage(t *testing.T, snapshot playlists.AggregateSnapshot) playlists.AggregatePage {
	t.Helper()
	p, err := playlists.AggregateSnapshotPage(snapshot, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPlaylistAggregateRetainsAllProvenanceAndIndependentContributions(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[2].ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	child := playlistCreate(t, s, "admin", "Child", localBinding(tracks[0].ID, tracks[1].ID, tracks[0].ID))
	parent := playlistCreate(t, s, "admin", "Parent", playlists.PlaylistBinding{Kind: "playlist", TargetID: &child.ID}, localBinding(tracks[2].ID))
	snapshot := playlistBuild(t, s, "admin", parent)
	if len(snapshot.Entries) != 2 || len(snapshot.Entries[0].Provenance) != 3 || snapshot.Entries[0].RecordingID == nil || *snapshot.Entries[0].RecordingID != *linked.RecordingID || *snapshot.Entries[1].Source.SourceTrackID != tracks[1].ID {
		t.Fatal("recording dedup lost source contributions")
	}
	if len(snapshot.Entries[0].Provenance[0].BindingPath) != 2 || snapshot.Entries[0].Provenance[0].OriginPlaylistID != child.ID {
		t.Fatal("nested provenance lost original source")
	}
	fresh, err := s.Playlist(ctx, "admin", parent.ID)
	if err != nil || fresh.Stale {
		t.Fatal("fresh aggregate incorrectly stale", err)
	}
	reused := playlistBuild(t, s, "admin", parent)
	if reused.Revision != snapshot.Revision {
		t.Fatal("unchanged rebuild replaced immutable snapshot")
	}
	updated, err := s.EditPlaylist(ctx, "admin", parent.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: parent.Name, Bindings: parent.Bindings[1:]}, "remove-child-once")
	if err != nil {
		t.Fatal(err)
	}
	after := playlistBuild(t, s, "admin", updated)
	if len(after.Entries) != 1 || *after.Entries[0].Source.SourceTrackID != tracks[2].ID || len(after.Entries[0].Provenance) != 1 {
		t.Fatal("removing one binding removed another contribution")
	}
	old, err := s.PlaylistSnapshot(ctx, "admin", parent.ID, snapshot.Revision)
	if err != nil || !reflect.DeepEqual(old, snapshot) {
		t.Fatal("historical aggregate was changed", err)
	}
	_, err = s.PlaylistSnapshot(ctx, "bob", parent.ID, snapshot.Revision)
	matchingError(t, err, "NOT_FOUND")
}

func TestPlaylistAggregateRemoteReferencesAndChildRevisionBinding(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	remote := remoteImport(t, s, "admin", remoteBinding())
	observed := remotePublish(t, s, remote, remoteObservation(1000, "song", "song"))
	child := playlistCreate(t, s, "admin", "Remote child", playlists.PlaylistBinding{Kind: "remote", TargetID: &remote.ID})
	parent := playlistCreate(t, s, "admin", "Local and remote", localBinding(tracks[0].ID), playlists.PlaylistBinding{Kind: "playlist", TargetID: &child.ID})
	first := playlistBuild(t, s, "admin", parent)
	if len(first.Entries) != 2 || len(first.Entries[1].Provenance) != 2 || first.Entries[1].RecordingID != nil || *first.Entries[1].Source.RemoteSourceID != observed.Entries[0].Source.ID {
		t.Fatal("remote occurrences were lost or falsely matched")
	}
	remotePublish(t, s, remote, remoteObservation(1001, "song", "next"))
	p, err := s.Playlist(ctx, "admin", parent.ID)
	if err != nil || !p.Stale {
		t.Fatal("remote revision did not stale ancestors", err)
	}
	second := playlistBuild(t, s, "admin", parent)
	if second.Revision <= first.Revision || len(second.Entries) != 3 || second.Dependencies[0].Revision == first.Dependencies[0].Revision {
		t.Fatal("rebuild did not capture new child snapshot")
	}
	_, err = s.PlaylistSnapshot(ctx, "other", parent.ID, 1)
	matchingError(t, err, "NOT_FOUND")
}

func TestPlaylistRulesRejectCyclesRetargetingAndConcurrentLostUpdates(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := context.Background()
	private := matchingPrivateTrack(t, s)
	_, err := s.CreatePlaylist(ctx, "admin", playlists.CreatePlaylistRequest{Name: "Forbidden", Bindings: []playlists.PlaylistBinding{localBinding(private.ID)}}, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
	a := playlistCreate(t, s, "admin", "A", localBinding(tracks[0].ID))
	b := playlistCreate(t, s, "admin", "B", playlists.PlaylistBinding{Kind: "playlist", TargetID: &a.ID})
	_, err = s.EditPlaylist(ctx, "admin", a.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: a.Name, Bindings: []playlists.PlaylistBinding{{Kind: "playlist", TargetID: &b.ID}}}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	unchanged, err := s.Playlist(ctx, "admin", a.ID)
	if err != nil || unchanged.RuleRevision != 1 || len(unchanged.Bindings) != 1 {
		t.Fatal("cycle left partial rule edit", err)
	}
	retarget := a.Bindings
	retarget[0].Entries[0].Source.SourceTrackID = playlistString(tracks[1].ID)
	_, err = s.EditPlaylist(ctx, "admin", a.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: a.Name, Bindings: retarget}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	var wg sync.WaitGroup
	out := make(chan error, 2)
	for _, name := range []string{"first", "second"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			_, e := s.EditPlaylist(ctx, "admin", a.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: name, Bindings: unchanged.Bindings}, domain.NewID())
			out <- e
		}(name)
	}
	wg.Wait()
	close(out)
	success := 0
	for err := range out {
		if err == nil {
			success++
		} else {
			matchingError(t, err, "CONFLICT")
		}
	}
	if success != 1 {
		t.Fatal("playlist edit lost expected revision")
	}
}
