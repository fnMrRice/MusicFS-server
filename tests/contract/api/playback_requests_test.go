package api_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPlaybackRejectsMissingAndUnexpectedControlFields(t *testing.T) {
	server, _ := testServer()
	// Invalid inputs must fail before calling the playback application service.
	server.RegisterPlayback(nil)
	cookie, session := login(t, server)
	for _, test := range []struct {
		path string
		body string
	}{
		{"/api/v1/playback/sessions", `{"sourceTrackId":"track","capabilities":{"htmlAudio":true,"formats":[]}}`},
		{"/api/v1/playback/sessions", `{"sourceTrackId":"track","preferences":{}}`},
		{"/api/v1/playback/sessions", `{"sourceTrackId":"track","capabilities":{"htmlAudio":true},"preferences":{}}`},
		{"/api/v1/playback/sessions", `{"sourceTrackId":"track","capabilities":{"htmlAudio":true,"formats":[{"mime":"audio/wav"}]},"preferences":{}}`},
		{"/api/v1/playback/sessions", `{"sourceTrackId":"track","capabilities":{"htmlAudio":true,"formats":[{"mime":"audio/wav","canPlay":"yes"}]},"preferences":{}}`},
		{"/api/v1/playback/sessions/session/seek", `{"attemptId":"attempt"}`},
		{"/api/v1/playback/sessions/session/seek", `{"attemptId":"attempt","positionMs":null}`},
		{"/api/v1/playback/sessions/session/seek", `{"attemptId":"attempt","positionMs":0,"observedError":null}`},
		{"/api/v1/playback/sessions/session/recover", `{"attemptId":"attempt","positionMs":0}`},
		{"/api/v1/playback/sessions/session/recover", `{"attemptId":"attempt","positionMs":0,"observedError":{"code":"MEDIA_ERROR"}}`},
		{"/api/v1/playback/sessions/session/recover", `{"attemptId":"attempt","positionMs":0,"observedError":{"code":"MEDIA_ERROR","message":"` + strings.Repeat("x", 1025) + `"}}`},
	} {
		r := httptest.NewRequest("POST", test.path, strings.NewReader(test.body))
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "strict-contract-key")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("%s returned %d for %s", test.path, w.Code, test.body)
		}
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
}
