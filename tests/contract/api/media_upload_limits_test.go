package api_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
)

type mediaDeadlineRecorder struct {
	*httptest.ResponseRecorder
	deadlines      []time.Time
	writeDeadlines []time.Time
}

func (w *mediaDeadlineRecorder) SetReadDeadline(value time.Time) error {
	w.deadlines = append(w.deadlines, value)
	return nil
}

func (w *mediaDeadlineRecorder) SetWriteDeadline(value time.Time) error {
	w.writeDeadlines = append(w.writeDeadlines, value)
	return nil
}

func TestMediaDeliveryDeadlineAndCleanup(t *testing.T) {
	server, _ := testServer()
	cookie, _ := login(t, server)
	var deadline time.Time
	server.HandleMediaDelivery("GET /api/v1/media-budget", func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		deadline, _ = r.Context().Deadline()
		w.WriteHeader(http.StatusNoContent)
		return nil
	})
	for _, limit := range []time.Duration{time.Minute, 2 * time.Second} {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		r := httptest.NewRequest("GET", "/api/v1/media-budget", nil).WithContext(ctx)
		r.AddCookie(cookie)
		w := &mediaDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		server.ServeHTTP(w, r)
		cancel()
		if w.Code != 204 || len(w.writeDeadlines) != 2 || !w.writeDeadlines[0].Equal(deadline) || !w.writeDeadlines[1].IsZero() {
			t.Fatal("media delivery deadline was missing or leaked")
		}
		maximum := min(limit, api.MediaDeliveryTimeout)
		if remaining := time.Until(deadline); remaining > maximum || remaining < maximum-time.Second {
			t.Fatal("media delivery ignored total or parent deadline")
		}
	}
}

// This tests the special transport budget independently of content decoders.
// The ordinary control route must keep its original 64 KiB JSON/body budget.
func TestMediaUploadDedicatedBudgetAuthorizationAndDeadlineCleanup(t *testing.T) {
	server, _ := testServer()
	cookie, session := login(t, server)
	var received int64
	var deadline time.Time
	handler := func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		deadline, _ = r.Context().Deadline()
		n, err := io.Copy(io.Discard, r.Body)
		received = n
		if err != nil {
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
	server.HandleMediaUpload("POST /api/v1/upload-budget", handler)
	server.Handle("POST /api/v1/control-budget", true, true, handler)
	request := func(path string, body io.Reader, length int64, mutate func(*http.Request)) *mediaDeadlineRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1"+path, body)
		r.ContentLength = length
		r.AddCookie(cookie)
		r.Header.Set("Origin", origin)
		r.Header.Set("X-CSRF-Token", session.CSRFToken)
		if mutate != nil {
			mutate(r)
		}
		w := &mediaDeadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		server.ServeHTTP(w, r)
		return w
	}
	large := strings.Repeat("x", 128<<10)
	start := time.Now()
	w := request("/upload-budget", strings.NewReader(large), int64(len(large)), nil)
	if w.Code != 204 || received != int64(len(large)) || deadline.Sub(start) > 31*time.Second || deadline.Sub(start) < 29*time.Second {
		t.Fatal("media budget or deadline missing")
	}
	if len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Fatal("upload read deadline leaked into keep-alive request")
	}
	w = request("/control-budget", strings.NewReader(large), int64(len(large)), nil)
	if w.Code == 204 || received != api.MaxRequestBytes {
		t.Fatal("ordinary body limit was relaxed")
	}
	w = request("/upload-budget", strings.NewReader(""), api.MaxMediaUploadBytes+1, nil)
	if w.Code != 400 {
		t.Fatal("declared oversized upload accepted")
	}
	w = request("/upload-budget", io.LimitReader(repeatedByteReader{}, api.MaxMediaUploadBytes+1), -1, nil)
	if w.Code != 400 || received != api.MaxMediaUploadBytes {
		t.Fatal("chunked upload bypassed the maximum")
	}
	for _, mutate := range []func(*http.Request){func(r *http.Request) { r.Header.Del("Cookie") }, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, func(r *http.Request) { r.Header.Set("Origin", "https://untrusted.invalid") }} {
		received = -1
		w = request("/upload-budget", strings.NewReader("private body"), 12, mutate)
		if w.Code != 401 && w.Code != 403 || received != -1 {
			t.Fatal("upload body was processed before authorization")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	w = request("/upload-budget", strings.NewReader("ok"), 2, func(r *http.Request) { *r = *r.WithContext(ctx) })
	if w.Code != 204 || time.Until(deadline) > 2*time.Second {
		t.Fatal("upload extended the parent request deadline")
	}
	short, stop := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer stop()
	w = request("/upload-budget", cancelledMediaBody{short}, -1, func(r *http.Request) { *r = *r.WithContext(short) })
	if w.Code != 503 || len(w.deadlines) != 2 || !w.deadlines[1].IsZero() {
		t.Fatal("cancelled upload failed to release its deadline")
	}
}

type cancelledMediaBody struct{ ctx context.Context }

func (b cancelledMediaBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }

type repeatedByteReader struct{}

func (repeatedByteReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}
