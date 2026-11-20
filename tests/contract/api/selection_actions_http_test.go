package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/matching/standard"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestSelectionActionsHTTPContractsAndValidation(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	server.RegisterPersonal(personal.New(store))
	server.RegisterPlaylists(playlists.NewAggregate(store))
	server.RegisterMatching(matching.New(store, nil, standard.Plugin{}))
	cookie, session := login(t, server)
	ctx := context.Background()
	provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
	if _, _, err := store.Enqueue(ctx, "admin", provider, testkit.ScanPlan(provider), "selection-fixture"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for _, name := range []string{"a", "b", "c"} {
		items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: name, RootID: provider, RelativePath: "secret/" + name + ".wav", DisplayName: name, ObservedVersion: "v1", SizeBytes: 100}, Artists: []string{}})
	}
	if err = store.Publish(ctx, job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(ctx, job, len(items), nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListTracks(ctx, "admin", provider, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var revision string
	if err = store.DB.QueryRow("SELECT revision FROM library_revision").Scan(&revision); err != nil {
		t.Fatal(err)
	}
	selection := library.TrackSelection{Batch: true, LibraryRevision: revision, Exclude: []string{page.Items[0].ID}}
	post := func(path string, input any, key string, status int, schema string) []byte {
		t.Helper()
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("POST", "/api/v1"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s: %d %s", path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
		if strings.Contains(w.Body.String(), "secret/") {
			t.Fatal("response leaked a private path")
		}
		return w.Body.Bytes()
	}
	post("/source-tracks/selection", selection, "", 200, "TrackSelectionResult")
	favorite := map[string]any{"selection": selection, "favorite": true}
	post("/me/favorites/batch", favorite, "", 200, "FavoritesSelectionResult")
	post("/me/favorites/batch", map[string]any{"selection": selection}, "", 400, "Error")
	post("/me/favorites/batch", map[string]any{"favorite": false}, "", 400, "Error")
	create := playlists.CreateSelectionRequest{Name: "全选", Selection: selection}
	post("/playlists/selection", create, "", 400, "Error")
	raw := post("/playlists/selection", create, "selection-create", 201, "PlaylistSelectionResult")
	var receipt playlists.SelectionResult
	if err = json.Unmarshal(raw, &receipt); err != nil || receipt.Count != 2 {
		t.Fatal("invalid selection receipt", err)
	}
	if strings.Contains(string(raw), "bindings") {
		t.Fatal("selection response returned full bindings")
	}
	appendRequest := playlists.AppendSelectionRequest{ExpectedRevision: 1, Selection: library.TrackSelection{SourceTrackIDs: []string{page.Items[0].ID}}}
	post("/playlists/"+receipt.PlaylistID+"/selection", appendRequest, "selection-append", 200, "PlaylistSelectionResult")
	post("/playlists/"+receipt.PlaylistID+"/selection", appendRequest, "selection-stale", 409, "Error")
	link := matching.LinkRequest{Selection: &selection}
	post("/matching/links", link, "selection-link", 200, "MatchingMutation")
	link.SourceTrackIDs = []string{page.Items[0].ID}
	post("/matching/links", link, "selection-ambiguous", 400, "Error")
	selection.LibraryRevision = "0"
	post("/me/favorites/batch", map[string]any{"selection": selection, "favorite": false}, "", 409, "Error")
}

func TestTrackSelectionSchemaRejectsAmbiguousAndStaleInputs(t *testing.T) {
	schema := testkit.ContractSchema(t, "http", "TrackSelection")
	for _, raw := range []string{
		`{"batch":true}`,
		`{"batch":true,"libraryRevision":"1","sourceTrackIds":["track-a"]}`,
		`{"batch":false,"sourceTrackIds":["track-a"],"exclude":["track-b"]}`,
		`{"batch":false,"sourceTrackIds":["track-a"],"groupBy":"recording"}`,
	} {
		if err := schema.Validate(testkit.DecodeJSON(t, []byte(raw))); err == nil {
			t.Fatalf("invalid selection accepted: %s", raw)
		}
	}
}
