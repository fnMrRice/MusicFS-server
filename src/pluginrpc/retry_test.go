package pluginrpc

import (
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
)

func TestPersistencePreservesExplicitRetryMarker(t *testing.T) {
	for name, tc := range map[string]struct {
		failure  error
		attempts int
		status   string
	}{
		"marked transient":           {jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "test")), 1, "retry_wait"},
		"unmarked transient":         {domain.Fail("TEMPORARY_FAILURE", "test"), 1, "failed"},
		"budget exhausted":           {jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "test")), jobs.MaxAttempts, "failed"},
		"authorization still paused": {jobs.Retryable(domain.Fail("ACCESS_DENIED", "test")), 1, "paused"},
	} {
		t.Run(name, func(t *testing.T) {
			result := jobs.DecideRetry(decodeError(encodeError(tc.failure)), tc.attempts, time.Now())
			if result.Status != tc.status {
				t.Fatalf("retry semantics changed: got %s want %s", result.Status, tc.status)
			}
		})
	}
}
