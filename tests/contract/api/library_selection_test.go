package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestLibrarySelectionHTTPCompactReferencesAndRevisionGuard(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	provider := "316664dc-7106-5acd-8df7-88c9d0d9baef"
	if _, _, err := store.Enqueue(t.Context(), "admin", provider, testkit.ScanPlan(provider), "selection-http-fixture"); err != nil {
		t.Fatal(err)
	}
	job, err := store.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	items := []library.Observation{}
	for _, id := range []string{"one", "two"} {
		items = append(items, library.Observation{Observation: pluginproto.Observation{ExternalID: id, RootID: provider,
			RelativePath: "private/" + id + ".wav", ObservedVersion: "v1", DisplayName: id + ".wav", MIME: "audio/wav", SizeBytes: 100}, Artists: []string{}})
	}
	if err := store.Publish(t.Context(), job, testkit.ScanPublication(job, items)); err != nil {
		t.Fatal(err)
	}
	if err := store.Finish(t.Context(), job, 2, nil); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListGroupedTracks(t.Context(), "admin", provider, "", 200)
	if err != nil {
		t.Fatal(err)
	}
	selection := library.TrackSelection{Batch: true, ProviderInstanceID: provider, GroupBy: "recording",
		LibraryRevision: page.LibraryRevision, MatchingRevision: page.MatchingRevision, Exclude: []string{page.Items[0].ID}}
	request := func(value library.TrackSelection, authenticated, csrf bool) *httptest.ResponseRecorder {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(http.MethodPost, "/api/v1/source-tracks/selection", bytes.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		if authenticated {
			r.AddCookie(cookie)
		}
		if csrf {
			r.Header.Set("Origin", origin)
			r.Header.Set("X-CSRF-Token", session.CSRFToken)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	w := request(selection, true, true)
	if w.Code != http.StatusOK {
		t.Fatalf("selection returned %d: %s", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "TrackSelectionResult", w.Body.Bytes())
	var result library.TrackSelectionResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Total != 1 || len(result.Items) != 1 || result.Items[0].ID != page.Items[1].ID {
		t.Fatalf("invalid selection result: %s %v", w.Body.String(), err)
	}
	for _, forbidden := range []string{"versions", "private/", "\"asset\"", "\"codec\"", "\"recordingId\""} {
		if bytes.Contains(w.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("selection exposed expanded metadata: %s", forbidden)
		}
	}
	if w := request(selection, false, true); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous selection accepted: %d", w.Code)
	}
	if w := request(selection, true, false); w.Code != http.StatusForbidden {
		t.Fatalf("selection without CSRF accepted: %d", w.Code)
	}
	selection.LibraryRevision = "0"
	if w := request(selection, true, true); w.Code != http.StatusConflict {
		t.Fatalf("stale selection accepted: %d %s", w.Code, w.Body.String())
	}
}
