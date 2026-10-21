package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestCatalogHTTPAuthenticationValidationFixedQueryAndPrivatePaths(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	server.RegisterCatalog(catalog.New(store))
	ctx := context.Background()
	if _, _, err := store.Enqueue(ctx, "admin", "316664dc-7106-5acd-8df7-88c9d0d9baef", testkit.ScanPlan("316664dc-7106-5acd-8df7-88c9d0d9baef"), "catalog-http-fixture"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for _, name := range []string{"one", "two"} {
		items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: "316664dc-7106-5acd-8df7-88c9d0d9baef", RelativePath: "secret-folder/" + name + ".wav", DisplayName: name, ObservedVersion: "v1"}, Artists: []string{"Artist"}})
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, 2, nil); err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body, key string, authenticated, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewBufferString(body))
		if authenticated {
			r.AddCookie(cookie)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", origin)
		r.Header.Set("Idempotency-Key", key)
		if csrf {
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		method, path, body, key string
		auth, csrf              bool
		status                  int
	}{
		{"GET", "/library/search", "", "", false, false, 401},
		{"GET", "/library/search?type=playlists", "", "", true, false, 400},
		{"GET", "/library/search?sort=name&sort=title", "", "", true, false, 400},
		{"GET", "/library/search?genres=Rock&genres=Jazz", "", "", true, false, 200},
		{"GET", "/library/search?favoriteOnly=1", "", "", true, false, 400},
		{"GET", "/library/search?playableOnly=true", "", "", true, false, 400},
		{"GET", "/library/search?limit=0", "", "", true, false, 400},
		{"GET", "/library/search?years=abc", "", "", true, false, 400},
		{"GET", "/library/search?minBitrateBps=300&maxBitrateBps=100", "", "", true, false, 400},
		{"POST", "/library/queries", `{"query":{}}`, "catalog-no-csrf", true, false, 403},
		{"POST", "/library/queries", `{"query":{}}`, "", true, true, 400},
		{"POST", "/library/queries", `{}`, "catalog-empty-input", true, true, 400},
		{"POST", "/library/queries", `{"query":{"unknown":1}}`, "catalog-unknown-input", true, true, 400},
	} {
		w := request(tc.method, tc.path, tc.body, tc.key, tc.auth, tc.csrf)
		if w.Code != tc.status {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body.String())
		}
	}
	created := request(http.MethodPost, "/library/queries", `{"query":{"type":"tracks","providerInstanceId":"316664dc-7106-5acd-8df7-88c9d0d9baef"},"limit":1}`, "catalog-query-key", true, true)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var page catalog.Page
	if err = json.Unmarshal(created.Body.Bytes(), &page); err != nil || page.Total != 2 || page.NextCursor == nil {
		t.Fatal(page, err)
	}
	if strings.Contains(created.Body.String(), "secret-folder") || strings.Contains(created.Body.String(), `"path"`) {
		t.Fatal("source path leaked")
	}
	read := request(http.MethodGet, "/library/queries/"+page.QueryID+"?cursor="+*page.NextCursor+"&limit=1", "", "", true, false)
	if read.Code != 200 {
		t.Fatal(read.Body.String())
	}
	wrong := request(http.MethodGet, "/library/search?providerInstanceId=843558e1-a158-56ac-834a-baffd12bb5c5&cursor="+*page.NextCursor, "", "", true, false)
	if wrong.Code != 410 {
		t.Fatal(wrong.Code, wrong.Body.String())
	}
	cancelled := request(http.MethodDelete, "/library/queries/"+page.QueryID, "", "", true, true)
	if cancelled.Code != 204 {
		t.Fatal(cancelled.Code, cancelled.Body.String())
	}
	read = request(http.MethodGet, "/library/queries/"+page.QueryID, "", "", true, false)
	if read.Code != 410 {
		t.Fatal(read.Code, read.Body.String())
	}
}
