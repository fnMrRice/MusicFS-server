package storage_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/matching"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func TestPlaylistHiddenAnchorRequiresConfirmationBeforeExpanding(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	ctx := context.Background()
	p := playlistCreate(t, s, "admin", "Hidden intention", localBinding(tracks[0].ID, tracks[1].ID, tracks[2].ID))
	hidden, err := s.HidePlaylistSource(ctx, "admin", p.ID, playlists.HideRequest{Scope: "allSources", Source: playlists.SourceRef{SourceTrackID: &tracks[0].ID}}, "hide-original-source")
	if err != nil {
		t.Fatal(err)
	}
	initial := playlistPage(t, playlistBuild(t, s, "admin", p))
	if len(initial.Items) != 2 || hidden.Items[0].NeedsConfirmation {
		t.Fatal("unmatched source hide broadened")
	}
	link, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	rules, err := s.HiddenRules(ctx, "admin", p.ID)
	if err != nil || !rules.Items[0].NeedsConfirmation {
		t.Fatal("first association silently expanded hidden group", err)
	}
	next := playlistPage(t, playlistBuild(t, s, "admin", p))
	if len(next.Items) != 2 || len(next.NeedsConfirmation) != 1 || *next.Items[0].Source.SourceTrackID != tracks[1].ID {
		t.Fatal("new recording member was hidden without confirmation")
	}
	confirmed, err := s.ChangePlaylistHidden(ctx, "admin", p.ID, hidden.Items[0].ID, playlists.ChangeHiddenRequest{ExpectedRevision: 1, Active: true, Confirm: true}, "confirm-expanded-group")
	if err != nil || confirmed.Items[0].NeedsConfirmation {
		t.Fatal(err)
	}
	all := playlistPage(t, playlistBuild(t, s, "admin", p))
	if len(all.Items) != 1 || *all.Items[0].Source.SourceTrackID != tracks[2].ID {
		t.Fatal("explicit confirmation did not apply")
	}
	if _, err = s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: link.Revision, SourceTrackID: tracks[0].ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	split := playlistPage(t, playlistBuild(t, s, "admin", p))
	if len(split.Items) != 2 {
		t.Fatal("split propagated hidden intent to every result")
	}
	for _, entry := range split.Items {
		if *entry.Source.SourceTrackID == tracks[0].ID {
			t.Fatal("split lost original hidden anchor")
		}
	}
	_, err = s.ChangePlaylistHidden(ctx, "admin", p.ID, hidden.Items[0].ID, playlists.ChangeHiddenRequest{ExpectedRevision: 1, Active: false}, domain.NewID())
	matchingError(t, err, "CONFLICT")
}

func TestPlaylistHiddenSingleOccurrenceAndBindingSurviveRebuild(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	p := playlistCreate(t, s, "admin", "Repeated", localBinding(tracks[0].ID, tracks[0].ID), localBinding(tracks[0].ID))
	first := p.Bindings[0]
	request := playlists.HideRequest{Scope: "entry", Source: playlists.SourceRef{SourceTrackID: &tracks[0].ID}, BindingPath: []string{first.ID}, EntryID: &first.Entries[0].ID}
	hidden, err := s.HidePlaylistSource(ctx, "admin", p.ID, request, "hide-one-occurrence")
	if err != nil {
		t.Fatal(err)
	}
	snapshot := playlistBuild(t, s, "admin", p)
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Hidden || len(snapshot.Entries[0].Provenance) != 3 || !snapshot.Entries[0].Provenance[0].Hidden || snapshot.Entries[0].Provenance[1].Hidden {
		t.Fatal("single occurrence hide affected another provenance")
	}
	_, err = s.HidePlaylistSource(ctx, "admin", p.ID, playlists.HideRequest{ExpectedRevision: 1, Scope: "binding", Source: request.Source, BindingPath: []string{first.ID}}, "hide-one-binding")
	if err != nil {
		t.Fatal(err)
	}
	snapshot = playlistBuild(t, s, "admin", p)
	if snapshot.Entries[0].Hidden || !snapshot.Entries[0].Provenance[1].Hidden || snapshot.Entries[0].Provenance[2].Hidden {
		t.Fatal("binding hide affected other binding")
	}
	edited, err := s.EditPlaylist(ctx, "admin", p.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: p.Name, Bindings: p.Bindings[1:]}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	visible := playlistPage(t, playlistBuild(t, s, "admin", edited))
	if len(visible.Items) != 1 || len(visible.Items[0].Provenance) != 1 {
		t.Fatal("removed binding erased other contribution")
	}
	rules, err := s.HiddenRules(ctx, "admin", p.ID)
	if err != nil || len(rules.Items) != 2 {
		t.Fatal("rebuild destroyed hidden intent", err)
	}
	replay, err := s.HidePlaylistSource(ctx, "admin", p.ID, request, "hide-one-occurrence")
	if err != nil || replay.Revision != hidden.Revision {
		t.Fatal("hidden replay changed original result", err)
	}
	_, err = s.HiddenRules(ctx, "bob", p.ID)
	matchingError(t, err, "NOT_FOUND")
}
