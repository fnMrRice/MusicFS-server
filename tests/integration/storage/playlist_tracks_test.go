package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestPlaylistMusicTrackKeepsOptionalSourceChoice(t *testing.T) {
	s, _, tracks := matchingFixture(t, 2)
	ctx := t.Context()
	draft, err := s.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: "Unreleased"}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	ref := playlists.SourceRef{TrackID: &draft.ID}
	p := playlistCreate(t, s, "admin", "New recordings", playlists.PlaylistBinding{Kind: "local",
		Entries: []playlists.LocalEntry{{Source: ref}}})
	assertRef := func(p playlists.Playlist, forced string) {
		t.Helper()
		snapshot := playlistBuild(t, s, "admin", p)
		if len(snapshot.Entries) != 1 {
			t.Fatal("Unexpected playlist entries", snapshot.Entries)
		}
		src := snapshot.Entries[0].Source
		if src.TrackID == nil || *src.TrackID != draft.ID || src.SourceTrackID != nil ||
			(forced == "") != (src.ForcedSourceID == nil) || src.ForcedSourceID != nil && *src.ForcedSourceID != forced {
			t.Fatal("Playlist reference changed unexpectedly", src)
		}
	}
	assertRef(p, "")
	edit := func(chosen *string) (playlists.Playlist, error) {
		b := p.Bindings[0]
		b.Entries = append([]playlists.LocalEntry(nil), b.Entries...)
		b.Entries[0].Source.ForcedSourceID = chosen
		return s.EditPlaylist(ctx, "admin", p.ID, playlists.EditPlaylistRequest{ExpectedRevision: p.RuleRevision,
			Name: p.Name, Bindings: []playlists.PlaylistBinding{b}}, domain.NewID())
	}
	if _, err = edit(&tracks[0].ID); err == nil {
		t.Fatal("Accepted a forced source belonging to another track")
	}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{TargetRecordingID: &draft.ID,
		SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	explicit := playlistCreate(t, s, "admin", "Contributions", localBinding(tracks[0].ID))
	var contribution int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM music_playlist_entries
		WHERE playlist_id=? AND track_id=? AND forced_source_id=? AND source_contribution=1`,
		explicit.ID, draft.ID, tracks[0].ID).Scan(&contribution); err != nil || contribution != 1 {
		t.Fatal("Explicit source contribution has no normalized playlist entry", contribution, err)
	}
	p, err = edit(&tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRef(p, tracks[0].ID)
	if _, err = s.Unlink(ctx, "admin", matching.UnlinkRequest{ExpectedRevision: linked.Revision, SourceTrackID: tracks[0].ID}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	reread, err := s.Playlist(ctx, "admin", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertRef(reread, tracks[0].ID)
	p, err = edit(nil)
	if err != nil {
		t.Fatal(err)
	}
	assertRef(p, "")
}
