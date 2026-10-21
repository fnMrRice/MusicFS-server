package storage_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func TestPlaylistGraphDepthAndOccurrenceIdentityBoundaries(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	leaf := playlistCreate(t, s, "admin", "Leaf", localBinding(tracks[0].ID))
	child := leaf
	for i := 1; i < playlists.MaxGraphDepth; i++ {
		child = playlistCreate(t, s, "admin", "Parent", playlists.PlaylistBinding{Kind: "playlist", TargetID: &child.ID})
	}
	_, err := s.CreatePlaylist(ctx, "admin", playlists.CreatePlaylistRequest{Name: "Too deep", Bindings: []playlists.PlaylistBinding{{Kind: "playlist", TargetID: &leaf.ID}, {Kind: "playlist", TargetID: &child.ID}}}, domain.NewID())
	matchingError(t, err, "RATE_LIMITED")
	snapshot := playlistBuild(t, s, "admin", child)
	if len(snapshot.Entries[0].Provenance[0].BindingPath) != playlists.MaxGraphDepth {
		t.Fatal("valid graph lost its binding path")
	}
	_, err = s.CreatePlaylist(ctx, "admin", playlists.CreatePlaylistRequest{Name: "Stolen IDs", Bindings: leaf.Bindings}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	fresh, err := s.Playlist(ctx, "admin", child.ID)
	if err != nil || fresh.Stale {
		t.Fatal("valid deep graph is stale", err)
	}
}

func TestPlaylistRebuildFailurePreservesEarlierSnapshot(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	p := playlistCreate(t, s, "admin", "Local", localBinding(tracks[0].ID))
	first := playlistBuild(t, s, "admin", p)
	remote := remoteImport(t, s, "admin", remoteBinding())
	p, err := s.EditPlaylist(ctx, "admin", p.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: p.Name, Bindings: append(p.Bindings, playlists.PlaylistBinding{Kind: "remote", TargetID: &remote.ID})}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	request := playlists.RebuildRequest{ExpectedRuleRevision: p.RuleRevision}
	_, err = s.RebuildPlaylist(ctx, "admin", p.ID, request, "retry-failed-build")
	matchingError(t, err, "TEMPORARY_FAILURE")
	old, err := s.PlaylistSnapshot(ctx, "admin", p.ID, first.Revision)
	if err != nil || len(old.Entries) != 1 {
		t.Fatal("failed build damaged the old snapshot", err)
	}
	remotePublish(t, s, remote, remoteObservation(1000, "song"))
	rebuilt, err := s.RebuildPlaylist(ctx, "admin", p.ID, request, "retry-failed-build")
	if err != nil || len(rebuilt.Entries) != 2 {
		t.Fatal("failed key could not retry", err)
	}
	replay, err := s.RebuildPlaylist(ctx, "admin", p.ID, request, "retry-failed-build")
	if err != nil || replay.Revision != rebuilt.Revision {
		t.Fatal("build replay changed snapshot", err)
	}
}
