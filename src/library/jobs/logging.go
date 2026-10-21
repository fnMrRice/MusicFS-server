package jobs

import (
	"context"
	"log/slog"
	"time"
)

func (s *Service) logFinished(job Job, observed int, elapsed time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	current, err := s.repo.GetJob(ctx, job.Owner, job.ID)
	if err != nil {
		slog.Warn("Scan task result unavailable", "jobId", job.ID, "errorCode", "TEMPORARY_FAILURE")
		return
	}
	level := slog.LevelInfo
	message := "Scan task state recorded"
	switch current.Status {
	case "succeeded":
		message = "Scan task completed"
	case "retry_wait":
		level, message = slog.LevelWarn, "Scan task retry scheduled"
	case "paused":
		level, message = slog.LevelWarn, "Scan task paused"
	case "failed":
		level, message = slog.LevelError, "Scan task failed"
	case "cancelled":
		message = "Scan task cancelled"
	}
	attrs := []any{"jobId", job.ID, "instanceId", job.ProviderInstanceID, "state", current.Status, "observed", observed, "attempt", job.AttemptCount, "durationMs", elapsed.Milliseconds()}
	if current.Error != nil {
		attrs = append(attrs, "errorCode", current.Error.Code)
	}
	if current.NextAttemptAt != nil {
		attrs = append(attrs, "nextAttemptAt", *current.NextAttemptAt)
	}
	slog.Log(ctx, level, message, attrs...)
}
