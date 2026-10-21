package pluginrpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
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
			client, _ := endpointPair(t)
			var job jobs.Job
			err := client.Invoke(context.Background(), "Echo", []any{jobs.Job{}, tc.failure}, []any{&job})
			result := jobs.DecideRetry(err, tc.attempts, time.Now())
			if result.Status != tc.status {
				t.Fatalf("retry semantics changed: got %s want %s", result.Status, tc.status)
			}
		})
	}
}
