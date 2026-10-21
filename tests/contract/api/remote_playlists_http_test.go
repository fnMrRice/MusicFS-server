package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type playlistHTTPProvider struct{ onlinehost.Provider }

func (*playlistHTTPProvider) ObservePlaylist(context.Context, string) (online.PlaylistObservation, error) {
	panic("import must not perform network reads")
}

func TestRemotePlaylistsHTTPImportSyncSnapshotPrivacyAndErrors(t *testing.T) {
	server, store, _ := libraryHTTP(t)
	cookie, session := login(t, server)
	host, err := onlinehost.New([]onlinehost.Runtime{
		{Owner: "admin", InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", Platform: online.Netease, ConfigRevision: "private-config-revision", Enabled: true, Provider: &playlistHTTPProvider{}},
		{Owner: "admin", InstanceID: "b5e86bbe-524b-53fc-bde9-bbe3cbb02a80", Platform: online.QQMusic, ConfigRevision: "private-config-revision", Enabled: true, Provider: &playlistHTTPProvider{}},
		{Owner: "other", InstanceID: "843558e1-a158-56ac-834a-baffd12bb5c5", Platform: online.Netease, ConfigRevision: "private-config-revision", Enabled: true, Provider: &playlistHTTPProvider{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	service, err := playlists.New(store, host.PlaylistRemote(), 1)
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterRemotePlaylists(service)
	request := func(method, path string, input any, key string, change func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		var raw []byte
		if input != nil {
			var err error
			raw, err = json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
		}
		r := httptest.NewRequest(method, "/api/v1"+path, bytes.NewReader(raw))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		if change != nil {
			change(r)
		}
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		for _, secret := range []string{"private-config-revision", "private upstream credential", `"owner"`, `"assetId"`, `"streamUrl"`, `"binding"`} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private or invented audio data leaked", w.Body.String())
			}
		}
		return w
	}
	expect := func(method, path string, input any, key string, status int, schema string) *httptest.ResponseRecorder {
		t.Helper()
		w := request(method, path, input, key, nil)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
		return w
	}
	ref := playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", AccountID: "12345", ExternalID: "91"}
	decodeImported := func(w *httptest.ResponseRecorder) playlists.RemotePlaylist {
		t.Helper()
		var result struct {
			Playlist playlists.RemotePlaylist `json:"playlist"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result.Playlist
	}
	imported := decodeImported(expect("POST", "/remote-playlists", ref, "import-key-1", 201, "RemotePlaylistImported"))
	if imported.Revision != 0 || imported.Title != "" || !imported.Stale || imported.LastSuccessAt != nil {
		t.Fatal("import claimed successful upstream read")
	}
	if replay := decodeImported(expect("POST", "/remote-playlists", ref, "import-key-1", 201, "RemotePlaylistImported")); replay.ID != imported.ID {
		t.Fatal("import replay identity changed")
	}
	otherRef := ref
	otherRef.ExternalID = "92"
	expect("POST", "/remote-playlists", otherRef, "import-key-1", 409, "Error")
	expect("POST", "/remote-playlists", map[string]any{"instanceId": "7fe223d0-a3bd-5e87-87f8-305dc26d9041", "externalId": "91"}, "missing-account", 400, "Error")
	expect("POST", "/remote-playlists", map[string]any{"instanceId": "7fe223d0-a3bd-5e87-87f8-305dc26d9041", "accountId": "", "externalId": "91", "url": "private"}, "forged-url", 400, "Error")
	for _, tc := range []struct {
		ref    playlists.RemoteRef
		status int
	}{{playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", AccountID: "other", ExternalID: "91"}, 403}, {playlists.RemoteRef{InstanceID: "843558e1-a158-56ac-834a-baffd12bb5c5", ExternalID: "91"}, 404}, {playlists.RemoteRef{InstanceID: "b5e86bbe-524b-53fc-bde9-bbe3cbb02a80", AccountID: "12345", ExternalID: "91"}, 415}, {playlists.RemoteRef{InstanceID: "7fe223d0-a3bd-5e87-87f8-305dc26d9041", ExternalID: "https://private.invalid"}, 400}} {
		expect("POST", "/remote-playlists", tc.ref, "invalid-ref", tc.status, "Error")
	}
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }} {
		w := request("POST", "/remote-playlists", ref, "denied-import", change)
		if w.Code < 400 {
			t.Fatal("unauthenticated mutation succeeded")
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
	expect("POST", "/remote-playlists", ref, "", 400, "Error")
	ctx := context.Background()
	private, _, err := store.ImportRemotePlaylist(ctx, "other", playlists.Binding{RemoteRef: playlists.RemoteRef{InstanceID: "843558e1-a158-56ac-834a-baffd12bb5c5", ExternalID: "91"}, Platform: online.Netease, ConfigRevision: "private-config-revision"}, "private-import")
	if err != nil {
		t.Fatal(err)
	}
	privateJob, _, err := store.QueueRemoteSync(ctx, "other", private.ID, 1, playlists.Binding{RemoteRef: private.RemoteRef, Platform: online.Netease, ConfigRevision: private.ConfigRevision}, "private-sync")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.CancelRemoteSync(ctx, "other", privateJob.ID); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/remote-playlists/" + private.ID, "/remote-playlist-sync-jobs/" + privateJob.ID, "/remote-playlists/" + private.ID + "/snapshots/1"} {
		expect("GET", path, nil, "", 404, "Error")
	}
	expect("POST", "/remote-playlist-sync-jobs/"+privateJob.ID+"/cancel", struct{}{}, "", 404, "Error")
	list := expect("GET", "/remote-playlists?limit=1", nil, "", 200, "RemotePlaylists")
	if strings.Contains(list.Body.String(), private.ID) {
		t.Fatal("private import listed")
	}
	for _, q := range []string{"?limit=0", "?limit=101", "?limit=1&limit=2", "?accountId=other", "?offset=0", "?limit=01"} {
		expect("GET", "/remote-playlists"+q, nil, "", 400, "Error")
	}
	base := "/remote-playlists/" + imported.ID
	queue := func(key string) string {
		t.Helper()
		w := expect("POST", base+"/sync-jobs", map[string]int{"expectedBindingRevision": 1}, key, 202, "HashAccepted")
		var v struct {
			ID string `json:"jobId"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v.ID
	}
	expect("POST", base+"/sync-jobs", struct{}{}, "missing-revision", 400, "Error")
	expect("POST", base+"/sync-jobs", map[string]int{"expectedBindingRevision": 2}, "wrong-revision", 409, "Error")
	jobID := queue("sync-cancelled")
	if queue("sync-cancelled") != jobID {
		t.Fatal("sync replay changed identity")
	}
	cancelled := expect("POST", "/remote-playlist-sync-jobs/"+jobID+"/cancel", struct{}{}, "", 200, "RemotePlaylistSyncJob")
	var firstCancel playlists.SyncJob
	_ = json.Unmarshal(cancelled.Body.Bytes(), &firstCancel)
	replayed := expect("POST", "/remote-playlist-sync-jobs/"+jobID+"/cancel", struct{}{}, "", 200, "RemotePlaylistSyncJob")
	var secondCancel playlists.SyncJob
	_ = json.Unmarshal(replayed.Body.Bytes(), &secondCancel)
	if firstCancel.State != "cancelled" || !firstCancel.UpdatedAt.Equal(secondCancel.UpdatedAt) {
		t.Fatal("cancel was not naturally idempotent")
	}
	jobID = queue("sync-success")
	ticket, err := store.ClaimRemoteSync(ctx)
	if err != nil || ticket.ID != jobID {
		t.Fatal("claim", err)
	}
	count := int64(3)
	observation := playlists.Observation{PlaylistObservation: online.PlaylistObservation{Playlist: online.Playlist{ExternalID: "91", Title: "Synthetic", SourceURL: "https://music.163.com/playlist?id=91", TrackCount: &count}, Entries: []online.PlaylistEntry{{Position: 0, ExternalTrackID: "42"}, {Position: 1, ExternalTrackID: "42"}, {Position: 2, ExternalTrackID: "43"}}, Complete: true, Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}}
	snapshot, err := store.FinishRemoteSync(ctx, ticket, observation)
	if err != nil {
		t.Fatal(err)
	}
	w := expect("GET", base+"/snapshots/"+strconv.FormatInt(snapshot.Revision, 10)+"?limit=2", nil, "", 200, "RemotePlaylistSnapshotPage")
	var page playlists.SnapshotPage
	_ = json.Unmarshal(w.Body.Bytes(), &page)
	if len(page.Items) != 2 || page.NextOffset == nil || *page.NextOffset != 2 || page.Items[0].ID == page.Items[1].ID || page.Items[0].Source.ID != page.Items[1].Source.ID || page.Items[0].Source.PlaybackAvailability != "unsupported" {
		t.Fatal("duplicate occurrence or metadata-only identity lost")
	}
	expect("GET", base+"/snapshots/1?offset=2&limit=2", nil, "", 200, "RemotePlaylistSnapshotPage")
	for _, suffix := range []string{"/snapshots/0", "/snapshots/01", "/snapshots/1?offset=-1", "/snapshots/1?offset=20001", "/snapshots/1?offset=0&offset=1", "/snapshots/1?after=x"} {
		expect("GET", base+suffix, nil, "", 400, "Error")
	}
	jobID = queue("sync-failed")
	ticket, err = store.ClaimRemoteSync(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.FailRemoteSync(ctx, ticket, &domain.Error{Code: "TEMPORARY_FAILURE", Message: "private upstream credential"}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{base, "/remote-playlist-sync-jobs/" + jobID} {
		schema := "RemotePlaylist"
		if strings.Contains(path, "sync-jobs") {
			schema = "RemotePlaylistSyncJob"
		}
		w := expect("GET", path, nil, "", 200, schema)
		if !strings.Contains(w.Body.String(), w.Header().Get("X-Request-ID")) {
			t.Fatal("nested error lacks current request ID")
		}
	}
	old := expect("GET", base+"/snapshots/1?limit=2", nil, "", 200, "RemotePlaylistSnapshotPage")
	if !bytes.Equal(old.Body.Bytes(), w.Body.Bytes()) {
		t.Fatal("failure rewrote successful snapshot")
	}
	tracks, err := store.ListTracks(ctx, "admin", "", "", 10)
	if err != nil || len(tracks.Items) != 0 {
		t.Fatal("remote metadata created audio assets", err)
	}
}

func (*playlistHTTPProvider) PlatformSession(context.Context) (online.AccountView, error) {
	return online.AccountView{State: "signedIn", AccountID: "12345", Revision: "fixture-generation"}, nil
}
