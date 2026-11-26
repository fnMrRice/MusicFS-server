package storage_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
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
	expectAlbumCover := func(want string) {
		t.Helper()
		albums, err := s.MusicAlbums(ctx, "admin", music.Query{Limit: 100})
		if err != nil || len(albums.Items) == 0 {
			t.Fatalf("album list lost visible artwork: %+v %v", albums.Items, err)
		}
		for _, entry := range albums.Items {
			if entry.CoverPath == nil || *entry.CoverPath != want {
				t.Fatalf("album %s artwork = %v, want %s", entry.ID, entry.CoverPath, want)
			}
		}
		album, err := s.MusicAlbum(ctx, "admin", albums.Items[0].ID)
		if err != nil || album.CoverPath == nil || *album.CoverPath != want {
			t.Fatalf("album detail lost artwork: %+v %v", album, err)
		}
	}
	expectAlbumCover(path)
	queue, err := s.ResolveTrackSelection(ctx, "admin", library.TrackSelection{SourceTrackIDs: []string{locals[0].ID}})
	if err != nil || len(queue.Items) != 1 || queue.Items[0].OnlineCoverPath != path {
		t.Fatalf("queue lost online artwork: %+v %v", queue, err)
	}
	favs, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 10)
	if err != nil || len(favs.Items) != 1 || favs.Items[0].Track == nil || favs.Items[0].Track.OnlineCoverPath != path {
		t.Fatalf("favorite lost artwork: %+v %v", favs, err)
	}
	// 播放自动缓存到曲目后，专辑仍可更新原始图。显式选定本地图片则优先显示该媒体对象。
	input := contentInput(tgt, "", "official cover")
	input.Metadata.Field, input.Metadata.Format, input.MIME = "artwork", "png", "image/png"
	input.Metadata.Origin = mediastore.Origin{Kind: "import", InstanceID: tgt.ProviderID, CandidateID: tgt.ExternalID}
	cached := contentPublish(t, s, "admin", input)
	slot := contentSelect(t, s, cached, 0, true)
	expectAlbumCover(path)
	contentSelect(t, s, cached, slot.Revision, false)
	expectAlbumCover("/api/v1/media/" + cached.ID)
}
