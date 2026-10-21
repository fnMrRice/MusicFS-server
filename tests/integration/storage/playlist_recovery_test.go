package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func TestPlaylistHiddenChildAndImmutableSnapshotSurviveRestart(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	child := playlistCreate(t, s, "admin", "Child", localBinding(tracks[0].ID))
	hidden, err := s.HidePlaylistSource(ctx, "admin", child.ID, playlists.HideRequest{Scope: "allSources", Source: playlists.SourceRef{SourceTrackID: &tracks[0].ID}}, "hide-child")
	if err != nil {
		t.Fatal(err)
	}
	parent := playlistCreate(t, s, "admin", "Parent", playlists.PlaylistBinding{Kind: "playlist", TargetID: &child.ID}, localBinding(tracks[0].ID))
	before := playlistBuild(t, s, "admin", parent)
	if len(before.Entries) != 1 || before.Entries[0].Hidden || len(before.Entries[0].Provenance) != 2 || !before.Entries[0].Provenance[0].Hidden || before.Entries[0].Provenance[1].Hidden {
		t.Fatal("child hide erased an independent parent contribution")
	}
	var sequence int
	var name, path string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.PlaylistSnapshot(ctx, "admin", parent.ID, before.Revision)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("restart changed the fixed snapshot", err)
	}
	rules, err := reopened.HiddenRules(ctx, "admin", child.ID)
	if err != nil || !reflect.DeepEqual(hidden, rules) {
		t.Fatal("restart lost original hidden rules", err)
	}
	_, err = reopened.ChangePlaylistHidden(ctx, "admin", child.ID, hidden.Items[0].ID, playlists.ChangeHiddenRequest{ExpectedRevision: 1, Active: false}, "unhide-child")
	if err != nil {
		t.Fatal(err)
	}
	stale, err := reopened.Playlist(ctx, "admin", parent.ID)
	if err != nil || !stale.Stale {
		t.Fatal("changed child rule did not stale the parent", err)
	}
	rebuilt := playlistBuild(t, reopened, "admin", parent)
	for _, provenance := range rebuilt.Entries[0].Provenance {
		if provenance.Hidden {
			t.Fatal("child unhide was lost during rebuild")
		}
	}
	old, err := reopened.PlaylistSnapshot(ctx, "admin", parent.ID, before.Revision)
	if err != nil || !reflect.DeepEqual(old, before) {
		t.Fatal("rebuild rewrote an old page", err)
	}
	changes, err := reopened.PlaylistChanges(ctx, "admin", child.ID, 0, 100)
	if err != nil || len(changes) != 3 || changes[0].Operation != "create" || changes[1].Operation != "hide" || changes[2].Operation != "changeHidden" || changes[2].HideRevision != 2 {
		t.Fatal("restart lost revision audit", err)
	}
	_, err = reopened.PlaylistChanges(ctx, "other", child.ID, 0, 100)
	matchingError(t, err, "NOT_FOUND")
}

func TestPlaylistSnapshotRechecksAllContributionOwners(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	p := playlistCreate(t, s, "admin", "Owner scope", localBinding(tracks[0].ID))
	request := playlists.RebuildRequest{ExpectedRuleRevision: p.RuleRevision}
	snapshot, err := s.RebuildPlaylist(ctx, "admin", p.ID, request, "guarded-snapshot")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate future ownership removal independently of the immutable snapshot.
	if _, err = s.DB.Exec("UPDATE tracks SET owner=? WHERE id=?", "other", tracks[0].ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.PlaylistSnapshot(ctx, "admin", p.ID, snapshot.Revision)
	matchingError(t, err, "NOT_FOUND")
	_, err = s.RebuildPlaylist(ctx, "admin", p.ID, request, "guarded-snapshot")
	matchingError(t, err, "NOT_FOUND")
	_, err = s.RebuildPlaylist(ctx, "admin", p.ID, request, domain.NewID())
	matchingError(t, err, "NOT_FOUND")
}
