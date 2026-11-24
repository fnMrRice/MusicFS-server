package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type indexedDetailsProvider struct {
	onlinehost.Provider
	done    chan struct{}
	details pluginproto.TrackDetails
	failure error
	calls   int
}

func (p *indexedDetailsProvider) Done() <-chan struct{} { return p.done }
func (p *indexedDetailsProvider) PlatformSession(context.Context) (online.AccountView, error) {
	return online.AccountView{State: "signedIn", AccountID: "verified", Revision: "current"}, nil
}
func (p *indexedDetailsProvider) TrackDetails(_ context.Context, id string) (pluginproto.TrackDetails, error) {
	p.calls++
	if id != "42" {
		return pluginproto.TrackDetails{}, domain.Fail("INVALID_ARGUMENT", "Unexpected track ID")
	}
	return p.details, p.failure
}

func TestIndexedOnlineTrackDetailsRouteAndFailures(t *testing.T) {
	const instance = "7fe223d0-a3bd-5e87-87f8-305dc26d9041"
	ctx := t.Context()
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	lib := library.New(store, jobs.New(store), nil, nil)
	provider := &indexedDetailsProvider{done: make(chan struct{}), details: pluginproto.TrackDetails{
		ExternalID: "42", Title: "Live title", Sections: []pluginproto.TrackDetailSection{},
	}}
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "admin", InstanceID: instance, Platform: online.Netease, ConfigRevision: "fixture", Enabled: true, Provider: provider}})
	if err != nil {
		t.Fatal(err)
	}
	if err = host.ConfigureAudio(lib); err != nil {
		t.Fatal(err)
	}
	publish := func(owner string) string {
		t.Helper()
		track, err := store.PublishOnlineTrack(ctx, owner, instance, online.Track{ExternalID: "42", Title: "Indexed title"})
		if err != nil {
			t.Fatal(err)
		}
		return track.ID
	}
	source, other := publish("admin"), publish("other")
	server, _ := testServer()
	server.RegisterLibrary(lib)
	cookie, _ := login(t, server)
	get := func(id string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/source-tracks/"+id+"/details", nil)
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		return w
	}
	checkCode := func(w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		var body struct {
			Error domain.Error `json:"error"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != status || body.Error.Code != code {
			t.Fatalf("expected %d %s, got %d %+v", status, code, w.Code, body)
		}
	}
	checkCode(get(other), http.StatusNotFound, "NOT_FOUND")
	if provider.calls != 0 {
		t.Fatal("foreign source reached provider")
	}
	ok := get(source)
	testkit.ValidateHTTP(t, "SourceTrackDetails", ok.Body.Bytes())
	var result library.TrackDetails
	if err := json.Unmarshal(ok.Body.Bytes(), &result); err != nil || ok.Code != 200 || result.SourceTrackID != source || result.ProviderInstanceID != instance || result.ExternalID != "42" || result.Title != "Live title" {
		t.Fatal(ok.Code, result, err)
	}
	delay := 7
	provider.failure = &domain.Error{Code: "RATE_LIMITED", Message: "Rate limited", RetryAfter: &delay}
	limited := get(source)
	checkCode(limited, http.StatusTooManyRequests, "RATE_LIMITED")
	if limited.Header().Get("Retry-After") != "7" {
		t.Fatal("missing retry delay")
	}
	provider.failure = nil
	provider.details.ExternalID = "another"
	var fault *domain.Error
	if _, err := lib.Details(ctx, "admin", source); !errors.As(err, &fault) || fault.Code != "PROTOCOL_MISMATCH" {
		t.Fatal("unrelated platform track was accepted", err)
	}
	close(provider.done)
	checkCode(get(source), http.StatusServiceUnavailable, "SOURCE_OFFLINE")
	if provider.calls != 3 {
		t.Fatal("offline provider was called", provider.calls)
	}
}
