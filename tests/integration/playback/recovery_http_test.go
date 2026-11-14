package playback_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestTimelineAndCrossSourceRecoveryAuthenticatedHTTP(t *testing.T) {
	f := recoverySetup(t, nil, 0)
	authentication := auth.New(f.repo.Store, "synthetic-secret", []string{"http://localhost"}, time.Hour)
	who, cookie, err := authentication.Login(context.Background(), "synthetic-secret", "http://localhost", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	f.who = who
	server := api.New(api.Options{Auth: authentication, Ready: func() bool { return true }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	server.RegisterPlayback(f.player)
	call := func(method, path string, value any, key string, csrf bool) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(value)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", key)
		req.Header.Set("Origin", "http://localhost")
		if csrf {
			req.Header.Set("X-CSRF-Token", who.CSRFToken)
		}
		req.AddCookie(&http.Cookie{Name: api.CookieName, Value: cookie})
		w := httptest.NewRecorder()
		server.ServeHTTP(w, req)
		return w
	}
	t.Run("native HTTP stream", func(t *testing.T) {
		request := f.request
		request.SourceTrackID = f.tracks[0].ID
		request.Preferences.ForceProcessing = false
		request.Capabilities.HTMLAudio = false
		request.Capabilities.NativeHTTP = true
		response := call("POST", "/api/v1/playback/sessions", request, "native-http-create", true)
		var description playback.Description
		if response.Code != http.StatusCreated || json.Unmarshal(response.Body.Bytes(), &description) != nil ||
			description.OutputProfile.Transport != "nativeHttp" {
			t.Fatalf("native playback create: %d %s", response.Code, response.Body.String())
		}
		testkit.ValidateHTTP(t, "Playback", response.Body.Bytes())
		expected, err := os.ReadFile(f.path)
		if err != nil {
			t.Fatal(err)
		}
		audio := call("GET", description.StreamURL, nil, "", false)
		if audio.Code != http.StatusOK || !bytes.Equal(audio.Body.Bytes(), expected) {
			t.Fatalf("unexpected native stream: %d (%d bytes)", audio.Code, audio.Body.Len())
		}
	})
	profileResponse := call("GET", "/api/v1/playback/recovery-profile", nil, "", false)
	var profile timeline.Profile
	if profileResponse.Code != 200 || json.Unmarshal(profileResponse.Body.Bytes(), &profile) != nil {
		t.Fatalf("profile HTTP: %d %s", profileResponse.Code, profileResponse.Body.String())
	}
	testkit.ValidateHTTP(t, "TimelineProfile", profileResponse.Body.Bytes())
	request := timeline.Request{RecordingID: f.recording, From: timelineTarget(f.tracks[0]), To: timelineTarget(f.tracks[1]), Range: timeline.Range{StartMs: 0, EndMs: 6000}, OffsetMs: 1000, Profile: profile}
	if got := call("POST", "/api/v1/timeline-evidence", request, "no-csrf-alignment", false); got.Code != 403 {
		t.Fatal("alignment bypassed CSRF")
	}
	created := call("POST", "/api/v1/timeline-evidence", request, "http-alignment", true)
	var e timeline.Evidence
	if created.Code != 201 || json.Unmarshal(created.Body.Bytes(), &e) != nil || e.Origin != "manualAlignment" {
		t.Fatalf("alignment HTTP: %d %s", created.Code, created.Body.String())
	}
	testkit.ValidateHTTP(t, "TimelineEvidence", created.Body.Bytes())
	encoded, _ := json.Marshal(request)
	var fields map[string]any
	json.Unmarshal(encoded, &fields)
	delete(fields, "offsetMs")
	if got := call("POST", "/api/v1/timeline-evidence", fields, "missing-offset", true); got.Code != 400 {
		t.Fatal("implicit alignment offset accepted")
	}
	json.Unmarshal(encoded, &fields)
	fields["owner"] = "another-owner"
	if got := call("POST", "/api/v1/timeline-evidence", fields, "injected-owner", true); got.Code != 400 {
		t.Fatal("client-provided owner accepted")
	}
	d := f.create(t, "http-playback-create")
	recovered := call("POST", "/api/v1/playback/sessions/"+d.SessionID+"/recover-source", recoveryRequest(d, f.tracks[1].ID, &e), "http-cross-recovery", true)
	var result playback.RecoveryResult
	if recovered.Code != 200 || json.Unmarshal(recovered.Body.Bytes(), &result) != nil || result.Decision != "continue" || result.Playback == nil || result.Playback.PositionOriginMs != 5000 {
		t.Fatalf("recovery HTTP: %d %s", recovered.Code, recovered.Body.String())
	}
	testkit.ValidateHTTP(t, "RecoveryResult", recovered.Body.Bytes())
	if got := call("GET", d.StreamURL, nil, "", false); got.Code != 409 {
		t.Fatalf("old URL stayed live: %d", got.Code)
	}
	revoked := call("POST", "/api/v1/timeline-evidence/"+e.ID+"/revoke", map[string]int64{"expectedRevision": 1}, "http-revoke-evidence", true)
	if revoked.Code != 200 {
		t.Fatalf("revoke HTTP: %d %s", revoked.Code, revoked.Body.String())
	}
	testkit.ValidateHTTP(t, "TimelineEvidence", revoked.Body.Bytes())
	if got := call("GET", "/api/v1/timeline-evidence/"+e.ID, nil, "", false); got.Code != 200 {
		t.Fatal("evidence history disappeared")
	}
}
