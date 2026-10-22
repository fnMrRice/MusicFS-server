package api_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
)

type failedFlushWriter struct {
	*httptest.ResponseRecorder
	err          error
	flushCalls   int
	headerWrites int
}

func (w *failedFlushWriter) WriteHeader(status int) {
	w.headerWrites++
	w.ResponseRecorder.WriteHeader(status)
}

func (w *failedFlushWriter) FlushError() error {
	w.flushCalls++
	return w.err
}

func TestStreamFlushErrorPropagatesAndAbortsWithoutJSON(t *testing.T) {
	for _, body := range []string{"", "AUDIO"} {
		t.Run("body="+body, func(t *testing.T) {
			server, _ := testServer()
			cookie, _ := login(t, server)
			flushErr := errors.New("stream flush failed")
			var receivedErr error
			server.HandleStream("GET /api/v1/flush", func(w http.ResponseWriter, _ *http.Request, _ auth.Session) error {
				w.Header().Set("Content-Type", "audio/mpeg")
				if body != "" {
					if _, err := w.Write([]byte(body)); err != nil {
						return err
					}
				}
				receivedErr = http.NewResponseController(w).Flush()
				return receivedErr
			})
			w := &failedFlushWriter{ResponseRecorder: httptest.NewRecorder(), err: flushErr}
			r := httptest.NewRequest(http.MethodGet, "/api/v1/flush", nil)
			r.AddCookie(cookie)
			var aborted any
			func() {
				defer func() { aborted = recover() }()
				server.ServeHTTP(w, r)
			}()
			if receivedErr != flushErr || w.flushCalls != 1 {
				t.Fatalf("flush error was not preserved: error=%v calls=%d", receivedErr, w.flushCalls)
			}
			if aborted != http.ErrAbortHandler {
				t.Fatalf("failed stream flush must abort: %v", aborted)
			}
			if w.Code != http.StatusOK || w.headerWrites != 1 || w.Body.String() != body || w.Header().Get("Content-Type") != "audio/mpeg" {
				t.Fatalf("failed stream flush appended an error response: status=%d headers=%d body=%q", w.Code, w.headerWrites, w.Body.String())
			}
		})
	}
}

func TestStreamFlushKeepsFlusherCompatibility(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "response-controller"
		if legacy {
			name = "http-flusher"
		}
		t.Run(name, func(t *testing.T) {
			server, _ := testServer()
			cookie, _ := login(t, server)
			server.HandleStream("GET /api/v1/flush", func(w http.ResponseWriter, _ *http.Request, _ auth.Session) error {
				if legacy {
					flusher, ok := w.(http.Flusher)
					if !ok {
						t.Fatal("stream response does not implement http.Flusher")
					}
					flusher.Flush()
				} else if err := http.NewResponseController(w).Flush(); err != nil {
					t.Fatalf("supported flush failed: %v", err)
				}
				return errors.New("stream interrupted after flush")
			})
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/flush", nil)
			r.AddCookie(cookie)
			var aborted any
			func() {
				defer func() { aborted = recover() }()
				server.ServeHTTP(w, r)
			}()
			if aborted != http.ErrAbortHandler || !w.Flushed || w.Code != http.StatusOK || w.Body.Len() != 0 {
				t.Fatalf("flush did not commit the stream response: abort=%v flushed=%v status=%d body=%q", aborted, w.Flushed, w.Code, w.Body.String())
			}
		})
	}
}
