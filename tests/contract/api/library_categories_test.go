package api_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestLibraryCategoriesHTTPBrowseAndMembers(t *testing.T) {
	server, store, cookie := libraryHTTP(t)
	server.RegisterCatalog(catalog.New(store))
	ctx := context.Background()
	provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
	if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan(provider), "category-http-fixture"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	album, genre := "专辑", "Rock"
	items := []library.Observation{
		{Observation: pluginproto.Observation{ExternalID: "one", RootID: provider, RelativePath: "音乐/one.flac", DisplayName: "one", ObservedVersion: "v1"}, Artists: []string{"歌手", "歌手"}, Album: &album, Genre: &genre},
		{Observation: pluginproto.Observation{ExternalID: "two", RootID: provider, RelativePath: "two.flac", DisplayName: "two", ObservedVersion: "v1"}, Artists: []string{}},
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, len(items), nil); err != nil {
		t.Fatal(err)
	}
	request := func(path string, auth bool, status int) []byte {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1"+path, nil)
		if auth {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		model := "CatalogPage"
		if status != 200 {
			model = "Error"
		}
		testkit.ValidateHTTP(t, model, w.Body.Bytes())
		return w.Body.Bytes()
	}
	request("/library/categories?categoryKind=artist", false, 401)
	for _, path := range []string{
		"/library/categories", "/library/categories?categoryKind=other",
		"/library/categories?categoryKind=artist&categoryKind=album",
		"/library/categories?categoryKind=artist&type=tracks",
		"/library/categories?categoryKind=artist&parentId=parent",
		"/library/categories?categoryKind=folder&parentId=parent&categoryId=item",
		"/library/categories?categoryKind=folder&limit=0",
		"/library/search?categoryId=item", "/library/search?categoryKind=artist",
		"/library/search?type=albums&categoryKind=album&categoryId=item",
	} {
		request(path, true, 400)
	}
	request("/library/categories?categoryKind=artist&categoryId=missing", true, 404)
	for _, kind := range []string{"artist", "album", "genre", "folder"} {
		t.Run(kind, func(t *testing.T) {
			query := url.Values{"categoryKind": {kind}, "providerInstanceId": {provider}, "limit": {"1"}}
			var page catalog.Page
			if err := json.Unmarshal(request("/library/categories?"+query.Encode(), true, 200), &page); err != nil {
				t.Fatal(err)
			}
			if len(page.Items) != 1 {
				t.Fatal("missing category")
			}
			var category catalog.Category
			if err := json.Unmarshal(page.Items[0], &category); err != nil || category.Kind != kind || category.TrackCount < 1 {
				t.Fatal(category, err)
			}
			members := url.Values{"categoryKind": {kind}, "categoryId": {category.ID}, "providerInstanceId": {provider}}
			var tracks catalog.Page
			if err := json.Unmarshal(request("/library/search?"+members.Encode(), true, 200), &tracks); err != nil || tracks.Total != category.TrackCount {
				t.Fatal("category count differs from members", tracks, err)
			}
			request("/library/categories?"+members.Encode(), true, 200)
			if page.NextCursor != nil {
				query.Set("cursor", *page.NextCursor)
				request("/library/categories?"+query.Encode(), true, 200)
				query.Set("q", "changed")
				request("/library/categories?"+query.Encode(), true, 410)
			}
		})
	}
}
