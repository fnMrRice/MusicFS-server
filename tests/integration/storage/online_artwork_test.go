package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

func TestNeteasePlaylistArtworkReachesCanonicalFavoritesAndQueue(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	locals := publishEvidenceTracks(t, s, catalogLocal("file-art", "Song"))
	b := remoteBinding()
	b.AccountID = "account"
	if _, err := s.LinkPlaylist(ctx, "admin", b, personal.Favorites); err != nil {
		t.Fatal(err)
	}
	job, err := s.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := remoteObservation(1000, "123")
	metadata := catalogOnline("123", "Song")
	cover := "https://p3.music.126.net/images/123.jpg"
	metadata.CoverURL = &cover
	o.Tracks[0].Track = &metadata
	if _, err = s.FinishRemoteSync(ctx, job, o); err != nil {
		t.Fatal(err)
	}
	var id string
	if err = s.DB.QueryRow("SELECT id FROM tracks WHERE external_id='123'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	tgt, err := s.FindTrack(ctx, "admin", id)
	path := "/api/v1/source-tracks/" + id + "/online-artwork"
	if err != nil || tgt.OnlineCoverPath != path || tgt.ProviderCoverURL != cover {
		t.Fatalf("import did not keep cover for %s: %v", id, err)
	}
	canonical, err := s.MusicTrack(ctx, "admin", *tgt.RecordingID)
	if err != nil || canonical.SelectedSource == nil || canonical.SelectedSource.OnlineCoverPath != path {
		t.Fatalf("canonical artwork missing: %+v %v", canonical, err)
	}
	queue, err := s.ResolveTrackSelection(ctx, "admin", library.TrackSelection{SourceTrackIDs: []string{locals[0].ID}})
	if err != nil || len(queue.Items) != 1 || queue.Items[0].OnlineCoverPath != path {
		t.Fatalf("queue lost online artwork: %+v %v", queue, err)
	}
	favs, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 10)
	if err != nil || len(favs.Items) != 1 || favs.Items[0].Track == nil || favs.Items[0].Track.OnlineCoverPath != path {
		t.Fatalf("favorite lost artwork: %+v %v", favs, err)
	}
}
