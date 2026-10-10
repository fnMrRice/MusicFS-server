package jobs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
)

func TestOnlyExplicitTransientErrorsRetryWithFiniteBudget(t *testing.T) {
	now := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name    string
		failure error
		want    string
	}{
		{"unclassified", errors.New("private path or credential"), "failed"},
		{"temporary without opt-in", domain.Fail("TEMPORARY_FAILURE", "Temporary failure"), "failed"},
		{"temporary", jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "Temporary failure")), "retry_wait"},
		{"rate", jobs.Retryable(domain.Fail("RATE_LIMITED", "Service capacity reached")), "retry_wait"},
		{"credentials", jobs.Retryable(domain.Fail("AUTH_REQUIRED", "Credentials expired")), "paused"},
		{"permission", domain.Fail("ACCESS_DENIED", "Access revoked"), "paused"},
		{"offline", domain.Fail("SOURCE_OFFLINE", "Provider unavailable"), "paused"},
		{"disabled", domain.Fail("PLUGIN_DISABLED", "Provider disabled"), "paused"},
		{"unsupported", jobs.Retryable(domain.Fail("UNSUPPORTED", "Unsupported media")), "failed"},
		{"changed", jobs.Retryable(domain.Fail("RESOURCE_CHANGED", "Source changed")), "failed"},
		{"cancel", jobs.Retryable(context.Canceled), "cancelled"},
	} {
		t.Run(test.name, func(t *testing.T) {
			d := jobs.DecideRetry(test.failure, 1, now)
			if d.Status != test.want || d.Error == nil {
				t.Fatalf("wrong decision: %+v", d)
			}
			if test.want == "retry_wait" {
				if d.NextAttemptAt == nil || d.NextAttemptAt.Sub(now) < time.Second || d.NextAttemptAt.Sub(now) > 1250*time.Millisecond {
					t.Fatalf("invalid first backoff: %+v", d)
				}
			} else if d.NextAttemptAt != nil {
				t.Fatal("terminal/paused error was scheduled")
			}
			if test.name == "unclassified" && d.Error.Message == test.failure.Error() {
				t.Fatal("private failure leaked")
			}
		})
	}
	for attempts := 1; attempts <= jobs.MaxAttempts; attempts++ {
		d := jobs.DecideRetry(jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "Temporary failure")), attempts, now)
		if attempts == jobs.MaxAttempts {
			if d.Status != "failed" || d.NextAttemptAt != nil {
				t.Fatal("retry budget was refreshed")
			}
		} else if d.Status != "retry_wait" {
			t.Fatal("retry budget ended early")
		}
	}
}

func TestRetryAfterCannotBecomeAHotLoopOrUnboundedTimer(t *testing.T) {
	now := time.Now().UTC()
	for _, seconds := range []int{-1, 0, 10, 300, 301} {
		err := &domain.Error{Code: "RATE_LIMITED", Message: "Capacity reached", RetryAfter: &seconds}
		d := jobs.DecideRetry(jobs.Retryable(err), 2, now)
		if seconds < 0 || seconds > 300 {
			if d.Status != "paused" || d.NextAttemptAt != nil {
				t.Fatal("invalid remote wait scheduled", seconds)
			}
			continue
		}
		if d.NextAttemptAt == nil || d.NextAttemptAt.Sub(now) < 2*time.Second || d.NextAttemptAt.Sub(now) < time.Duration(seconds)*time.Second || d.NextAttemptAt.Sub(now) > 5*time.Minute {
			t.Fatalf("invalid bounded wait: %d %+v", seconds, d)
		}
	}
}
