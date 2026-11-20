package online

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

func TestCatalogHTTPFailureWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name, hint, code string
		status           int
	}{
		{"short", "4", "RATE_LIMITED", 429},
		{"zero", "0", "RATE_LIMITED", 429},
		{"long", "120", "SOURCE_UNAVAILABLE", 503},
		{"missing", "", "RATE_LIMITED", 429},
		{"unavailable", "", "SOURCE_UNAVAILABLE", 503},
		{"forbidden", "", "SOURCE_UNAVAILABLE", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			attempts := 0
			client := &http.Client{Transport: lookupTestTransport(func(req *http.Request) (*http.Response, error) {
				attempts++
				w := httptest.NewRecorder()
				w.Header().Set("Retry-After", tc.hint)
				w.WriteHeader(tc.status)
				return w.Result(), nil
			})}
			host := tc.name + ".catalog-error.test"
			t.Cleanup(func() {
				catalogCooldown.Lock()
				delete(catalogCooldown.byHost, host)
				catalogCooldown.Unlock()
			})
			req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://"+host+"/search", nil)
			_, err := catalogRequest(client, req)
			var problem *fault.Error
			if !errors.As(err, &problem) || problem.Code != tc.code || problem.Scraper == nil || problem.Scraper.Reason != "upstream" || problem.Scraper.UpstreamStatus != tc.status || attempts != 1 {
				t.Fatalf("unexpected upstream failure: %#v attempts=%d", problem, attempts)
			}
			if tc.hint == "" {
				if problem.RetryAfter != nil {
					t.Fatal("unexpected retry hint")
				}
				return
			}
			if problem.RetryAfter == nil || strconv.Itoa(*problem.RetryAfter) != tc.hint {
				t.Fatalf("wrong upstream retry hint: %v", problem.RetryAfter)
			}
			if tc.hint == "0" {
				return
			}
			_, err = catalogRequest(client, req)
			if !errors.As(err, &problem) || problem.Code != "RATE_LIMITED" || problem.Scraper == nil || problem.Scraper.Reason != "cooldown" || problem.Scraper.UpstreamStatus != tc.status || problem.RetryAfter == nil || *problem.RetryAfter <= 0 || attempts != 1 {
				t.Fatalf("unexpected cooldown failure: %#v attempts=%d", problem, attempts)
			}
		})
	}
}
