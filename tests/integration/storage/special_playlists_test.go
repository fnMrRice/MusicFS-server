package storage_test

import (
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"testing"
	"time"
)

func TestSpecialPlaylistCanonicalFavoriteSourcesAndPlaybackMember(t *testing.T) {
	s, _, sources := matchingFixture(t, 3)
	ctx := t.Context()
	track, err := s.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: "Archive"}}, "song-without-source")
	if err != nil {
		t.Fatal(err)
	}
	favorite, err := s.PutFavorite(ctx, "admin", track.ID)
	if err != nil || favorite.Track != nil || favorite.MusicTrack.ID != track.ID {
		t.Fatal(favorite, err)
	}
	ids := []string{sources[0].ID, sources[1].ID}
	if _, err = s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids, TargetRecordingID: &track.ID}, "attach-existing-sources"); err != nil {
		t.Fatal(err)
	}
	chosen := sources[1].ID
	favorite, err = s.SetFavoriteSource(ctx, "admin", track.ID, &chosen)
	if err != nil || favorite.Track == nil || favorite.Track.ID != chosen {
		t.Fatal(favorite, err)
	}
	duplicate, err := s.PutFavorite(ctx, "admin", sources[0].ID)
	if err != nil || duplicate.ForcedSourceID == nil || *duplicate.ForcedSourceID != chosen || duplicate.RecordedAt != favorite.RecordedAt {
		t.Fatal(duplicate, err)
	}
	p, err := s.Playlist(ctx, "admin", personal.Favorites)
	if err != nil || p.Kind != personal.Favorites {
		t.Fatal(p, err)
	}
	snapshot, err := s.PlaylistSnapshot(ctx, "admin", p.ID, p.SnapshotRevision)
	if err != nil || len(snapshot.Entries) != 1 || *snapshot.Entries[0].Source.TrackID != track.ID || *snapshot.Entries[0].Source.ForcedSourceID != chosen {
		t.Fatal(snapshot, err)
	}
	if _, err = s.SetFavoriteSource(ctx, "admin", track.ID, &sources[2].ID); err == nil {
		t.Fatal("unrelated favorite source accepted")
	}
	if err = s.CommitPlayback(ctx, "playback", "login", "admin", []byte("{}"), time.Now().Add(time.Minute), "test", "member-bound-playback", "digest", track.ID, sources[2].ID); err == nil {
		t.Fatal("unrelated playback source accepted")
	}
	if _, err = s.SetFavoriteSource(ctx, "admin", track.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PlaylistSnapshot(ctx, "admin", p.ID, p.SnapshotRevision); err == nil {
		t.Fatal("expired special playlist snapshot accepted")
	}
}
