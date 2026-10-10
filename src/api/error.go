package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func classifyError(err error) (string, int) {
	var failure *domain.Error
	if errors.As(err, &failure) {
		switch failure.Code {
		case "INVALID_ARGUMENT":
			return failure.Code, http.StatusBadRequest
		case "AUTH_REQUIRED":
			return failure.Code, http.StatusUnauthorized
		case "ACCESS_DENIED", "PLATFORM_AUTH_REQUIRED":
			return failure.Code, http.StatusForbidden
		case "PRECONDITION_FAILED":
			return failure.Code, http.StatusPreconditionFailed
		case "NOT_FOUND":
			return failure.Code, http.StatusNotFound
		case "CONFLICT", "STALE_ATTEMPT", "RESOURCE_CHANGED", "SESSION_STOPPED", "RESTART_REQUIRED", "RECOVERY_EXHAUSTED":
			return failure.Code, http.StatusConflict
		case "CURSOR_EXPIRED", "SESSION_EXPIRED", "IDEMPOTENCY_EXPIRED":
			return failure.Code, http.StatusGone
		case "UNSUPPORTED":
			return failure.Code, http.StatusUnsupportedMediaType
		case "RATE_LIMITED":
			return failure.Code, http.StatusTooManyRequests
		case "SOURCE_OFFLINE", "SOURCE_UNAVAILABLE", "TEMPORARY_FAILURE", "PLUGIN_DISABLED", "CONFIG_READ_ONLY", "CONFIG_NOT_APPLIED":
			return failure.Code, http.StatusServiceUnavailable
		}
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "TEMPORARY_FAILURE", http.StatusServiceUnavailable
	}
	return "TEMPORARY_FAILURE", http.StatusInternalServerError
}

// WriteError exposes only known domain errors, never raw infrastructure errors.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	code, status := classifyError(err)
	detail := domain.Error{Code: code, Message: "Request could not be completed", RequestID: RequestID(r)}
	var failure *domain.Error
	if errors.As(err, &failure) && code == failure.Code {
		detail.Message = failure.Message
		detail.RetryAfter = failure.RetryAfter
	}
	if detail.RetryAfter != nil && *detail.RetryAfter >= 0 {
		w.Header().Set("Retry-After", strconv.Itoa(*detail.RetryAfter))
	}
	if status == http.StatusUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer realm=\"MusicFS\"")
	}
	_ = WriteJSON(w, status, struct {
		Error domain.Error `json:"error"`
	}{detail})
}
