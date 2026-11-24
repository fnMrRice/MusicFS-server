package storage_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestMergedTrackAlbumCreditsKeepMembershipAndSourceContext(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	local := publishEvidenceTracks(t, s, catalogLocal("file", "Song"))[0]
	metadata := catalogOnline("314", "Song")
	metadata.Album = "ORIGINAL   ALBUM"
	remote := catalogPublish(t, s, metadata)
	credits := func(id string) music.Track {
		t.Helper()
		track, err := s.MusicTrackForSource(ctx, "admin", id)
		if err != nil || len(track.Sources) != 2 || len(track.AlbumIDs) != 2 || len(track.Albums) != 1 {
			t.Fatalf("unexpected track credits: %+v, %v", track, err)
		}
		return track
	}
	track, onlineTrack := credits(local.ID), credits(remote.ID)
	localAlbum, onlineAlbum := track.Albums[0], onlineTrack.Albums[0]
	if localAlbum.ID == onlineAlbum.ID || localAlbum.Name != "Original album" || onlineAlbum.Name != metadata.Album {
		t.Fatal("source preferred the wrong credit", localAlbum, onlineAlbum)
	}
	for _, a := range []music.Album{localAlbum, onlineAlbum} {
		rows := albumRows(t, s, a.ID, "")
		if len(rows) != 1 || rows[0].ID != track.ID || len(rows[0].Albums) != 1 || rows[0].Albums[0].ID != a.ID || !slices.Contains(track.AlbumIDs, a.ID) {
			t.Fatal("album view lost its own membership", a, rows)
		}
	}
	page, err := s.MusicTracks(ctx, "admin", music.Query{ProviderID: "netease", Limit: 10})
	if err != nil || len(page.Items) != 1 || page.Items[0].Albums[0].ID != onlineAlbum.ID {
		t.Fatal("wrong album in provider view", page, err)
	}
	if _, err := s.PutFavorite(ctx, "admin", track.ID); err != nil {
		t.Fatal(err)
	}
	favorites, err := s.PersonalCollection(ctx, "admin", personal.Favorites, "", 10)
	if err != nil || len(favorites.Items) != 1 {
		t.Fatal(favorites, err)
	}
	fav := favorites.Items[0].MusicTrack
	if len(fav.Albums) != 1 || fav.Albums[0].ID != min(localAlbum.ID, onlineAlbum.ID) || len(fav.AlbumIDs) != 2 {
		t.Fatal("favorite has duplicate credits or missing membership", fav)
	}
	service := catalog.New(s)
	roots := categorySearch(t, service, catalog.Query{CategoryKind: "folder", ProviderInstanceID: "local"})
	if len(roots) != 1 {
		t.Fatal(roots)
	}
	files, err := service.Search(ctx, "admin", catalog.Query{CategoryKind: "folder", CategoryID: roots[0].ID}, "", 10)
	if err != nil || len(files.Items) != 1 {
		t.Fatal(files, err)
	}
	var file catalog.Track
	if err := json.Unmarshal(files.Items[0], &file); err != nil || len(file.Albums) != 1 || file.Albums[0].ID != localAlbum.ID {
		t.Fatal("folder row has the wrong album", file, err)
	}
	// 一张标签专辑有手工成员后优先展示；另一张仍能查询、导航和跟随合并修订。
	donor := metadataTrack(t, s, "Manual song", localAlbum.ID)
	current := credits(remote.ID)
	if current.Albums[0].ID != localAlbum.ID {
		t.Fatal("manually maintained album did not take precedence", current.Albums)
	}
	onlineRows := albumRows(t, s, onlineAlbum.ID, "")
	if len(onlineRows) != 1 || len(onlineRows[0].Albums) != 1 || onlineRows[0].Albums[0].ID != onlineAlbum.ID || !slices.Equal(onlineRows[0].AlbumIDs, current.AlbumIDs) {
		t.Fatal("filtered album should show its own credit and keep complete membership", onlineRows)
	}
	album, err := s.MusicAlbum(ctx, "admin", localAlbum.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := mergeTrackRequest(current, album.Revision, donor)
	request.ExpectedMatchingRevision, err = s.MatchingRevision(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	merged, err := s.MergeMusicAlbumTracks(ctx, "admin", album.ID, request, "merge-with-passive-album")
	if err != nil || len(merged.Albums) != 1 || !slices.Equal(merged.AlbumIDs, track.AlbumIDs) {
		t.Fatal("merge lost a folded album membership", merged, err)
	}
	after, err := s.MusicAlbum(ctx, "admin", onlineAlbum.ID)
	if err != nil || after.Revision <= onlineAlbum.Revision {
		t.Fatal("hidden album revision was not advanced", after, err)
	}
}

func TestAlbumCreditsPreserveCuratedReleasesWithTheSameName(t *testing.T) {
	s, ctx := catalogStore(t), t.Context()
	local := publishEvidenceTracks(t, s, catalogLocal("file", "Song"))[0]
	catalogPublish(t, s, catalogOnline("159", "Song"))
	track, err := s.MusicTrackForSource(ctx, "admin", local.ID)
	if err != nil || len(track.Albums) != 1 {
		t.Fatal(track, err)
	}
	tag := track.Albums[0]
	_, err = s.SelectMusicAlbumScrapedField(ctx, "admin", tag.ID, tag.Revision, works.ScrapedField{
		Field: "albumDetails", PluginID: "scraper.manual", CandidateToken: domain.NewID(),
		Item: scraper.Item{ID: "edition", Title: tag.Name},
	}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	var explicit []string
	for _, name := range []string{tag.Name, tag.Name + " (Deluxe Edition)"} {
		album, err := s.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: name}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		explicit = append(explicit, album.ID)
	}
	result, err := s.SaveMusicTrack(ctx, "admin", track.ID, track.Revision, music.TrackInput{
		TrackFields: track.TrackFields, ArtistNames: []string{"Artist"}, AlbumIDs: explicit,
	}, domain.NewID())
	if err != nil || len(result.Albums) != 3 || len(result.AlbumIDs) != 4 {
		t.Fatal("curated release was folded or a passive tag was retained", result, err)
	}
	for _, id := range append(explicit, tag.ID) {
		if !slices.ContainsFunc(result.Albums, func(a music.Album) bool { return a.ID == id }) {
			t.Fatal("curated release missing", id, result.Albums)
		}
	}
}
