package storage_test

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
)

func categorySearch(t *testing.T, service *catalog.Service, query catalog.Query) []catalog.Category {
	t.Helper()
	query.Type = "categories"
	page, err := service.Search(context.Background(), "admin", query, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]catalog.Category, len(page.Items))
	for i, raw := range page.Items {
		if err = json.Unmarshal(raw, &result[i]); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func categoryFind(t *testing.T, categories []catalog.Category, provider, name string, unknown bool) catalog.Category {
	t.Helper()
	for _, category := range categories {
		if category.ProviderInstanceID == provider && category.Unknown == unknown && (unknown || category.Name == name) {
			return category
		}
	}
	t.Fatalf("category not found: %s %s unknown=%v in %+v", provider, name, unknown, categories)
	return catalog.Category{}
}

func categoryUpdateTrack(t *testing.T, store *sqlite.Store, track library.Track) {
	t.Helper()
	raw, err := json.Marshal(track)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DB.ExecContext(context.Background(), "UPDATE tracks SET document=? WHERE id=?", raw, track.ID); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogCategoriesTagsUnknownFallbackAndSourceCounts(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	rock, lowerRock, blank, recording := " Rock ", "rock", " \t ", "same-recording"
	tracks[0].Artists, tracks[0].Genre = []string{"同名歌手", "同名歌手", " "}, &rock
	tracks[1].Artists, tracks[1].Album, tracks[1].Genre = []string{" ", "\t"}, &blank, &lowerRock
	tracks[2].Genre = &blank
	for _, track := range tracks {
		track.RecordingID = &recording
		categoryUpdateTrack(t, s, track)
	}
	firstAlbum := albumCreate(t, s, "admin", "local", "显式专辑", tracks[0].ID, tracks[1].ID)
	albumCreate(t, s, "admin", "local", "显式专辑", tracks[1].ID)
	albumCreate(t, s, "admin", "local", "额外专辑", tracks[1].ID)

	artists := categorySearch(t, service, catalog.Query{CategoryKind: "artist"})
	if len(artists) != 4 {
		t.Fatal(artists)
	}
	localArtist := categoryFind(t, artists, "local", "同名歌手", false)
	otherArtist := categoryFind(t, artists, "second", "同名歌手", false)
	if localArtist.ID == otherArtist.ID || localArtist.TrackCount != 1 || otherArtist.TrackCount != 2 || categoryFind(t, artists, "local", "", true).TrackCount != 2 {
		t.Fatal(artists)
	}
	albums := categorySearch(t, service, catalog.Query{CategoryKind: "album", ProviderInstanceID: "local"})
	if len(albums) != 4 {
		t.Fatal(albums)
	}
	for _, category := range albums {
		if category.TrackCount != 1 || len(category.Breadcrumbs) != 1 || category.Breadcrumbs[0].ID != category.ID {
			t.Fatal(category)
		}
		members, err := service.Search(ctx, "admin", catalog.Query{CategoryKind: "album", CategoryID: category.ID}, "", 200)
		if err != nil || members.Total != 1 {
			t.Fatal(members, err)
		}
	}
	if categoryFind(t, albums, "local", "显式专辑", false).ID == firstAlbum.ID {
		t.Fatal("album category reused source-album identity")
	}
	_, err := service.Search(ctx, "admin", catalog.Query{CategoryKind: "album", CategoryID: firstAlbum.ID}, "", 200)
	matchingError(t, err, "NOT_FOUND")
	genres := categorySearch(t, service, catalog.Query{CategoryKind: "genre"})
	if len(genres) != 2 || categoryFind(t, genres, "", "Rock", false).TrackCount != 4 || categoryFind(t, genres, "", "", true).TrackCount != 2 {
		t.Fatal(genres)
	}
	for _, kind := range []string{"artist", "album", "genre"} {
		categories := categorySearch(t, service, catalog.Query{CategoryKind: kind, ProviderInstanceID: "local"})
		provider := "local"
		if kind == "genre" {
			provider = ""
		}
		unknown := categoryFind(t, categories, provider, "", true)
		page, err := service.Search(ctx, "admin", catalog.Query{CategoryKind: kind, CategoryID: unknown.ID, ProviderInstanceID: "local"}, "", 200)
		if err != nil || page.Total != unknown.TrackCount {
			t.Fatal(page, err)
		}
	}
	facets, err := service.Facets(ctx, "admin", "local")
	if err != nil || facets.UnknownCounts["artist"] != 2 || facets.UnknownCounts["album"] != 1 || facets.UnknownCounts["genre"] != 1 || len(facets.Genres) != 1 || facets.Genres[0].TrackCount != 2 {
		t.Fatal(facets, err)
	}
}

func TestCatalogCategoriesFilteringSearchAndIdentityIsolation(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	if _, err := s.DB.ExecContext(ctx, "UPDATE tracks SET document=json_set(document,'$.container','mp3','$.availability','missing') WHERE id=?", tracks[1].ID); err != nil {
		t.Fatal(err)
	}
	artist := categoryFind(t, categorySearch(t, service, catalog.Query{CategoryKind: "artist"}), "local", "同名歌手", false)
	q := catalog.Query{CategoryKind: "artist", ProviderInstanceID: "local", Formats: []string{"flac"}, Q: "同名"}
	if got := categorySearch(t, service, q); len(got) != 1 || got[0].TrackCount != 1 || got[0].ID != artist.ID {
		t.Fatal(got)
	}
	q.Q = "unrelated"
	if got := categorySearch(t, service, q); len(got) != 0 {
		t.Fatal("category search matched track title", got)
	}
	for _, query := range []catalog.Query{
		{CategoryKind: "artist", CategoryID: artist.ID, Formats: []string{"flac"}},
		{CategoryKind: "artist", CategoryID: artist.ID, AvailableOnly: true},
		{CategoryKind: "artist", CategoryID: artist.ID, Availability: []string{"missing"}},
		{CategoryKind: "artist", CategoryID: artist.ID, Q: "selected"},
		{CategoryKind: "artist", CategoryID: artist.ID, Genres: []string{" ROCK "}, Availability: []string{"missing"}},
	} {
		page, err := service.Search(ctx, "admin", query, "", 200)
		if err != nil || page.Total != 1 {
			t.Fatal(query, page, err)
		}
	}
	for _, resultType := range []string{"tracks", "categories"} {
		query := catalog.Query{Type: resultType, CategoryKind: "artist", CategoryID: artist.ID, Formats: []string{"wav"}}
		page, err := service.Search(ctx, "admin", query, "", 200)
		if err != nil || page.Total != 0 || page.Items == nil {
			t.Fatal("existing category became missing after filtering", page, err)
		}
		query.CategoryID = "missing"
		_, err = service.Search(ctx, "admin", query, "", 200)
		matchingError(t, err, "NOT_FOUND")
		query.CategoryID = artist.ID
		_, err = service.Search(ctx, "bob", query, "", 200)
		matchingError(t, err, "NOT_FOUND")
		query.ProviderInstanceID = "second"
		_, err = service.Search(ctx, "admin", query, "", 200)
		matchingError(t, err, "NOT_FOUND")
	}
	if got := categorySearch(t, service, catalog.Query{CategoryKind: "artist", CategoryID: artist.ID}); len(got) != 1 || got[0].ID != artist.ID {
		t.Fatal(got)
	}
}

func TestCatalogFolderCategoriesTreeBreadcrumbsAndUnsafePaths(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	q := catalog.Query{CategoryKind: "folder", ProviderInstanceID: "local"}
	roots := categorySearch(t, service, q)
	if len(roots) != 1 || roots[0].ParentID != nil || roots[0].TrackCount != 3 || len(roots[0].Breadcrumbs) != 1 {
		t.Fatal(roots)
	}
	q.ParentID = roots[0].ID
	children := categorySearch(t, service, q)
	if len(children) != 1 || children[0].Name != "private-path" || children[0].TrackCount != 2 || len(children[0].Breadcrumbs) != 2 {
		t.Fatal(children)
	}
	q.ParentID = children[0].ID
	nested := categorySearch(t, service, q)
	if len(nested) != 1 || nested[0].Name != "nested" || len(nested[0].Breadcrumbs) != 3 || nested[0].Breadcrumbs[0].ID != roots[0].ID || nested[0].Breadcrumbs[2].ID != nested[0].ID {
		t.Fatal(nested)
	}
	q.ParentID, q.CategoryID = "", nested[0].ID
	if got := categorySearch(t, service, q); len(got) != 1 || got[0].ID != nested[0].ID {
		t.Fatal("detail lookup incorrectly applied root filter", got)
	}
	for _, category := range []catalog.Category{roots[0], children[0], nested[0]} {
		page, err := service.Search(ctx, "admin", catalog.Query{CategoryKind: "folder", CategoryID: category.ID}, "", 200)
		if err != nil || page.Total != category.TrackCount {
			t.Fatal(page, err)
		}
	}
	q.CategoryID, q.ParentID = "", "missing"
	_, err := service.Search(ctx, "admin", catalog.Query{Type: "categories", CategoryKind: "folder", ParentID: q.ParentID}, "", 200)
	matchingError(t, err, "NOT_FOUND")
	for _, unsafe := range []struct{ root, path string }{
		{"", "safe/a.flac"}, {" ", "safe/a.flac"}, {"music", ""}, {"music", "/secret/a.flac"},
		{"/secret", "safe/a.flac"}, {`C:\secret`, "safe/a.flac"}, {"music", "safe/./a.flac"},
		{"music", `C:\secret\a.flac`}, {"music", `\\server\secret\a.flac`}, {"music", "../secret/a.flac"},
		{"music", "safe/../secret/a.flac"}, {"music", "safe/../../a.flac"}, {"music", "secret\x00/a.flac"},
	} {
		if _, err = s.DB.ExecContext(ctx, "UPDATE tracks SET root_id=?,path=? WHERE owner='admin' AND provider='local'", unsafe.root, unsafe.path); err != nil {
			t.Fatal(err)
		}
		if got := categorySearch(t, service, catalog.Query{CategoryKind: "folder", ProviderInstanceID: "local"}); len(got) != 0 {
			t.Fatal("unsafe path created directories", unsafe, got)
		}
		facets, err := service.Facets(ctx, "admin", "local")
		if err != nil || len(facets.Directories) != 0 {
			t.Fatal("unsafe path leaked through facets", facets, err)
		}
	}
	if _, err = s.DB.ExecContext(ctx, "UPDATE tracks SET root_id='music',path='safe\\child\\a.flac' WHERE id=?", tracks[0].ID); err != nil {
		t.Fatal(err)
	}
	root := categorySearch(t, service, catalog.Query{CategoryKind: "folder", ProviderInstanceID: "local"})
	if len(root) != 1 || root[0].TrackCount != 1 || root[0].ID != roots[0].ID {
		t.Fatal(root)
	}
}

func TestCatalogCategorySnapshotsAndCursorBinding(t *testing.T) {
	s, service, tracks := catalogFixture(t)
	ctx := context.Background()
	query := catalog.Query{Type: "categories", CategoryKind: "artist"}
	first, err := service.Search(ctx, "admin", query, "", 1)
	if err != nil || first.Total != 4 || first.NextCursor == nil {
		t.Fatal(first, err)
	}
	before, err := service.Page(ctx, "admin", first.QueryID, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	tracks[0].Artists = []string{"Changed"}
	categoryUpdateTrack(t, s, tracks[0])
	after, err := service.Page(ctx, "admin", first.QueryID, "", 200)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("category snapshot was not frozen", after, err)
	}
	second, err := service.Search(ctx, "admin", query, *first.NextCursor, 200)
	if err != nil || second.Total != first.Total || second.CatalogRevision != first.CatalogRevision || !reflect.DeepEqual(second.Items, before.Items[1:]) {
		t.Fatal(second, err)
	}
	for _, changed := range []catalog.Query{
		{Type: "categories", CategoryKind: "album"}, {Type: "categories", CategoryKind: "artist", ProviderInstanceID: "local"},
		{Type: "categories", CategoryKind: "artist", Q: "同名"}, {Type: "categories", CategoryKind: "artist", Order: "desc"},
	} {
		_, err = service.Search(ctx, "admin", changed, *first.NextCursor, 1)
		matchingError(t, err, "CURSOR_EXPIRED")
	}
	_, err = service.Search(ctx, "bob", query, *first.NextCursor, 1)
	matchingError(t, err, "CURSOR_EXPIRED")
	_, err = service.Page(ctx, "bob", first.QueryID, "", 1)
	matchingError(t, err, "NOT_FOUND")
	latest, err := service.Search(ctx, "admin", query, "", 200)
	if err != nil || latest.Total != 5 || !strings.Contains(string(latest.Items[0]), "Changed") {
		t.Fatal(latest, err)
	}
}
