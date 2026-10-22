package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestSourceTrackDetailsUsesOwnedPluginObservationAndRejectsStaleFiles(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	file := filepath.Join(root, "中文来源.wav")
	fixture, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, fixture, 0600); err != nil {
		t.Fatal(err)
	}
	const instance = "316664dc-7106-5acd-8df7-88c9d0d9baef"
	worker, err := pluginhost.Start(ctx, pluginhost.Config{Builtin: true, UserID: "admin", InstanceID: instance, StateDir: t.TempDir(), Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { worker.Close() })
	plan := testkit.ScanPlan("music")
	plan.Roots[0].RootIdentity = worker.Access.Roots()[0].ExpectedIdentity
	scope, err := plan.Scope("music")
	if err != nil {
		t.Fatal(err)
	}
	page, err := worker.Scan(ctx, pluginproto.ScanRequest{RootID: "music", ScanID: "details-scan", Generation: 1, Limit: 200, Scope: scope, PlanDigest: strings.Repeat("a", 64)})
	if err != nil || len(page.Items) != 1 {
		t.Fatal(page, err)
	}
	observation := page.Items[0]
	if _, err = worker.TrackDetails(ctx, pluginproto.ResolveRequest{ExternalID: observation.ExternalID, ExpectedVersion: "changed"}); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
		t.Fatalf("stale plugin observation was accepted: %v", err)
	}
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	ids := map[string]string{}
	for _, owner := range []string{"admin", "other"} {
		provider := instance
		if owner == "other" {
			provider = "843558e1-a158-56ac-834a-baffd12bb5c5"
		}
		if _, _, err = store.Enqueue(ctx, owner, provider, plan, "details-fixture-"+owner); err != nil {
			t.Fatal(err)
		}
		job, err := store.Claim(ctx)
		if err != nil {
			t.Fatal(err)
		}
		hostTitle := "host-only-title"
		if err = store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{{Observation: observation, Title: &hostTitle, Artists: []string{}}})); err != nil {
			t.Fatal(err)
		}
		if err = store.Finish(ctx, job, 1, nil); err != nil {
			t.Fatal(err)
		}
		tracks, err := store.ListTracks(ctx, owner, "", "", 20)
		if err != nil || len(tracks.Items) != 1 {
			t.Fatal(tracks, err)
		}
		ids[owner] = tracks.Items[0].ID
	}
	service := library.New(store, jobs.New(store), []library.Runtime{{ID: instance, Owner: "admin", Worker: worker}}, nil)
	server, _ := testServer()
	server.RegisterLibrary(service)
	cookie, _ := login(t, server)
	request := func(id string, authenticated bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/source-tracks/"+id+"/details", nil)
		if authenticated {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		return w
	}
	for _, tc := range []struct {
		id     string
		auth   bool
		status int
	}{{ids["admin"], false, 401}, {ids["other"], true, 404}, {"missing", true, 404}} {
		w := request(tc.id, tc.auth)
		if w.Code != tc.status {
			t.Fatalf("authorization status %d: %s", w.Code, w.Body.String())
		}
	}
	w := request(ids["admin"], true)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "SourceTrackDetails", w.Body.Bytes())
	var details library.TrackDetails
	if err = json.Unmarshal(w.Body.Bytes(), &details); err != nil || details.Title != "中文来源.wav" || details.ExternalID != observation.ExternalID || details.SourceTrackID != ids["admin"] || details.ProviderInstanceID != instance {
		t.Fatal(details, err)
	}
	if strings.Contains(w.Body.String(), "host-only-title") || strings.Contains(w.Body.String(), "Temp") {
		t.Fatal("host metadata or absolute paths replaced plugin detail fields")
	}
	fields := map[string]string{}
	for _, section := range details.Sections {
		for _, field := range section.Fields {
			fields[field.ID] = field.Value
		}
	}
	if fields["relativePath"] != "中文来源.wav" || fields["root"] != "music" || fields["size"] == "" {
		t.Fatal("plugin source fields were lost", fields)
	}
	if err = os.WriteFile(file, append(fixture, 0), 0600); err != nil {
		t.Fatal(err)
	}
	if w = request(ids["admin"], true); w.Code != 409 || !strings.Contains(w.Body.String(), "RESOURCE_CHANGED") {
		t.Fatal("modified file returned stale details", w.Code, w.Body.String())
	}
	if err = os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if w = request(ids["admin"], true); w.Code != 404 {
		t.Fatal("missing file returned stale details", w.Code, w.Body.String())
	}
	worker.Close()
	if w = request(ids["admin"], true); w.Code != 503 {
		t.Fatal("offline plugin returned fabricated details", w.Code, w.Body.String())
	}
}
