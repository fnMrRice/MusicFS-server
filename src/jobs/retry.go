package jobs

import (
	"context"
	"errors"
	"math/rand/v2"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const MaxAttempts = 3

type retryableError struct{ cause error }

func (e *retryableError) Error() string { return e.cause.Error() }
func (e *retryableError) Unwrap() error { return e.cause }

// Retryable is an explicit host decision, not inferred from an arbitrary error
// string. Credentials, authorization and changed-resource failures never retry.
func Retryable(err error) error {
	if err == nil {
		return nil
	}
	return &retryableError{cause: err}
}

type RetryDecision struct {
	Status        string
	Error         *domain.Error
	NextAttemptAt *time.Time
}

func IsTerminal(status string) bool {
	switch status {
	case "succeeded", "failed", "cancelled", "paused":
		return true
	}
	return false
}

// DecideRetry persists a concrete UTC deadline. Replays and restarts reuse it
// instead of drawing another delay or replenishing the execution budget.
func DecideRetry(failure error, attempts int, now time.Time) RetryDecision {
	if failure == nil {
		return RetryDecision{Status: "succeeded"}
	}
	detail := domain.Error{Code: "TEMPORARY_FAILURE", Message: "Task failed; previous library retained"}
	var known *domain.Error
	if errors.As(failure, &known) {
		detail = *known
		detail.RequestID = ""
		if known.RetryAfter != nil {
			seconds := *known.RetryAfter
			detail.RetryAfter = &seconds
		}
	}
	invalidWait := detail.RetryAfter != nil && (*detail.RetryAfter < 0 || *detail.RetryAfter > 300)
	if invalidWait {
		detail.RetryAfter = nil
	}
	d := RetryDecision{Status: "failed", Error: &detail}
	if errors.Is(failure, context.Canceled) || detail.Code == "CANCELED" {
		d.Status = "cancelled"
		detail.Code, detail.Message = "CANCELED", "Task cancelled; previous library retained"
		return d
	}
	switch detail.Code {
	case "AUTH_REQUIRED", "ACCESS_DENIED", "SOURCE_OFFLINE":
		d.Status = "paused"
		return d
	}
	var marked *retryableError
	if !errors.As(failure, &marked) || (detail.Code != "TEMPORARY_FAILURE" && detail.Code != "RATE_LIMITED") || attempts < 1 || attempts >= MaxAttempts {
		return d
	}
	if invalidWait {
		d.Status = "paused"
		return d
	}
	delay := time.Second * time.Duration(1<<(attempts-1))
	if detail.RetryAfter != nil {
		if minimum := time.Duration(*detail.RetryAfter) * time.Second; minimum > delay {
			delay = minimum
		}
	}
	delay += time.Duration(rand.Uint64N(uint64(delay/4) + 1))
	if delay > 5*time.Minute {
		delay = 5 * time.Minute
	}
	next := now.UTC().Add(delay)
	d.Status, d.NextAttemptAt = "retry_wait", &next
	return d
}
