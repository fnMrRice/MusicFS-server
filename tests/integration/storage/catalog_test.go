package storage_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/catalog"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func catalogFixture(t *testing.T) (*sqlite.Store, *catalog.Service, []library.Track) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	for _, scope := range []struct{ owner, provider string }{{"admin", "local"}, {"admin", "second"}, {"bob", "private"}} {
		if _, _, err = s.Enqueue(ctx, scope.owner, scope.provider, testkit.ScanPlan("music"), domain.NewID()); err != nil {
			t.Fatal(err)
		}
		job, err := s.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		album, genre, format := "专辑一", "Rock", "flac"
		year, rate, duration := int64(2024), int64(48000), int64(42000)
		items := []library.Observation{}
		for i, name := range []string{"A unrelated.flac", "B selected.flac", "C selected.flac"} {
			observation := library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: "music", RelativePath: "private-path/nested/" + name, ObservedVersion: "v1", DisplayName: name, SizeBytes: 100, DurationMs: &duration}, Title: &name, Artists: []string{"同名歌手"}, Album: &album, Genre: &genre, Year: &year, SampleRateHz: &rate, Container: &format}
			if i == 2 {
				observation.Year = nil
				observation.Genre = nil
				observation.SampleRateHz = nil
				observation.Album = nil
				observation.Artists = []string{}
				observation.RelativePath = name
			}
			items = append(items, observation)
		}
		if err = s.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
			t.Fatal(err)
		}
		if err = s.Finish(ctx, job, len(items), nil); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListTracks(ctx, "admin", "local", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	return s, catalog.New(s), page.Items
}

func TestCatalogFullQuerySnapshotIdentityFilteringAndCancellation(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	query := catalog.Query{ProviderInstanceID: "local", Q: "selected", Sort: "name", Order: "desc"}
	first, err := service.Create(ctx, "admin", catalog.CreateRequest{Query: query, Limit: 1}, "query-first-once")
	if err != nil || first.Total != 2 || len(first.Items) != 1 || first.NextCursor == nil {
		t.Fatal(first, err)
	}
	var track catalog.Track
	if err = json.Unmarshal(first.Items[0], &track); err != nil || track.ID != tracks[2].ID {
		t.Fatal(track, err)
	}
	if track.Source.SourceTrackID == nil || *track.Source.SourceTrackID != track.ID || track.Source.RemoteSourceID != nil {
		t.Fatal("missing stable local source reference")
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE tracks SET document=json_set(document,'$.displayName','changed') WHERE owner='admin'"); err != nil {
		t.Fatal(err)
	}
	second, err := service.Page(ctx, "admin", first.QueryID, *first.NextCursor, 1)
	if err != nil || second.CatalogRevision != first.CatalogRevision || second.Total != 2 || len(second.Items) != 1 || second.NextCursor != nil || !strings.Contains(string(second.Items[0]), "B selected.flac") {
		t.Fatal(second, err)
	}
	replay, err := service.Create(ctx, "admin", catalog.CreateRequest{Query: query, Limit: 1}, "query-first-once")
	if err != nil || replay.QueryID != first.QueryID {
		t.Fatal(replay, err)
	}
	_, err = service.Search(ctx, "admin", catalog.Query{ProviderInstanceID: "local", Q: "unrelated"}, *first.NextCursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	_, err = service.Page(ctx, "bob", first.QueryID, "", 1)
	matchingError(t, err, "NOT_FOUND")
	_, err = service.Page(ctx, "bob", first.QueryID, *first.NextCursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	_, err = service.Create(ctx, "admin", catalog.CreateRequest{Query: catalog.Query{}}, "query-first-once")
	matchingError(t, err, "CONFLICT")
	if err = service.Cancel(ctx, "bob", first.QueryID); err == nil {
		t.Fatal("foreign cancel succeeded")
	}
	if err = service.Cancel(ctx, "admin", first.QueryID); err != nil {
		t.Fatal(err)
	}
	if err = service.Cancel(ctx, "admin", first.QueryID); err != nil {
		t.Fatal("cancel replay", err)
	}
	_, err = service.Page(ctx, "admin", first.QueryID, "", 10)
	matchingError(t, err, "CURSOR_EXPIRED")
	_, err = service.Create(ctx, "admin", catalog.CreateRequest{Query: query}, "query-first-once")
	matchingError(t, err, "CURSOR_EXPIRED")
}

func TestCatalogAlbumsArtistsFacetsAndUnknownMetadata(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	album := albumCreate(t, s, "admin", "local", "同名专辑", tracks[0].ID, tracks[1].ID)
	other, err := s.ListTracks(ctx, "admin", "second", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	albumCreate(t, s, "admin", "second", "同名专辑", other.Items[0].ID)
	page, err := service.Search(ctx, "admin", catalog.Query{Type: "albums", Q: "同名专辑"}, "", 1)
	if err != nil || page.Total != 2 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	artists, err := service.Search(ctx, "admin", catalog.Query{Type: "artists", Q: "同名"}, "", 50)
	if err != nil || artists.Total != 2 {
		t.Fatal(artists, err)
	}
	var artist catalog.Artist
	for _, raw := range artists.Items {
		var a catalog.Artist
		if err = json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		if a.ProviderInstanceID == "local" {
			artist = a
		}
	}
	if artist.ID == "" || artist.IdentityKind != "localCredit" || artist.TrackCount != 2 || artist.AlbumCount != 1 {
		t.Fatal(artist)
	}
	detail, err := service.Artist(ctx, "admin", artist.ID)
	if err != nil || detail.ID != artist.ID {
		t.Fatal(detail, err)
	}
	_, err = service.Artist(ctx, "bob", artist.ID)
	matchingError(t, err, "NOT_FOUND")
	artistAlbums, err := service.Search(ctx, "admin", catalog.Query{Type: "albums", ArtistID: artist.ID}, "", 50)
	if err != nil || artistAlbums.Total != 1 {
		t.Fatal(artistAlbums, err)
	}
	filtered, err := service.Search(ctx, "admin", catalog.Query{ProviderInstanceID: "local", SourceAlbumID: album.ID, ArtistID: artist.ID, Genres: []string{"ROCK"}, Years: []int64{2024}, SampleRatesHz: []int64{48000}, Formats: []string{"flac"}, Q: "selected"}, "", 50)
	if err != nil || filtered.Total != 1 {
		t.Fatal(filtered, err)
	}
	unknown, err := service.Search(ctx, "admin", catalog.Query{ProviderInstanceID: "local", UnknownFields: []string{"genre", "year", "sampleRateHz"}}, "", 50)
	if err != nil || unknown.Total != 1 {
		t.Fatal(unknown, err)
	}
	facets, err := service.Facets(ctx, "admin", "local")
	if err != nil || len(facets.Genres) != 1 || len(facets.Years) != 1 || len(facets.SampleRatesHz) != 1 || facets.UnknownCounts["genre"] != 1 {
		t.Fatal(facets, err)
	}
	var directory string
	for _, d := range facets.Directories {
		if d.Name == "nested" {
			directory = d.ID
			if d.TrackCount != 2 || d.ParentID == nil {
				t.Fatal(d)
			}
		}
	}
	if directory == "" {
		t.Fatal("missing directory category")
	}
	filtered, err = service.Search(ctx, "admin", catalog.Query{DirectoryID: directory}, "", 100)
	if err != nil || filtered.Total != 2 {
		t.Fatal(filtered, err)
	}
	raw, _ := json.Marshal(filtered)
	if strings.Contains(string(raw), "private-path/") || strings.Contains(string(raw), "relativePath") || strings.Contains(string(raw), `"path"`) {
		t.Fatal("private path leaked")
	}
}

func TestCatalogFavoriteSnapshotExpiryEmptyAndCancelledContext(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, "INSERT INTO personal_favorites VALUES(?,?,?)", "admin", tracks[1].ID, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	page, err := service.Search(ctx, "admin", catalog.Query{FavoriteOnly: true}, "", 50)
	if err != nil || page.Total != 1 {
		t.Fatal(page, err)
	}
	if _, err = s.DB.ExecContext(ctx, "DELETE FROM personal_favorites WHERE owner='admin'"); err != nil {
		t.Fatal(err)
	}
	stable, err := service.Page(ctx, "admin", page.QueryID, "", 50)
	if err != nil || stable.Total != 1 {
		t.Fatal(stable, err)
	}
	empty, err := service.Search(ctx, "admin", catalog.Query{FavoriteOnly: true}, "", 50)
	if err != nil || empty.Total != 0 || empty.Items == nil || empty.NextCursor != nil {
		t.Fatal(empty, err)
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE catalog_queries SET expires_at=? WHERE id=?", time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano), page.QueryID); err != nil {
		t.Fatal(err)
	}
	_, err = service.Page(ctx, "admin", page.QueryID, "", 50)
	matchingError(t, err, "CURSOR_EXPIRED")
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = service.Search(cancelled, "admin", catalog.Query{}, "", 50); err == nil {
		t.Fatal("cancelled query succeeded")
	}
}
