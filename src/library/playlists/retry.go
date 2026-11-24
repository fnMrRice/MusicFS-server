package playlists

import (
	"context"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const remoteSyncTimeout = 30 * time.Minute

// 已耗尽的批次错误穿过外层观察调用，避免从第一页开始重复消耗同一重试预算。
type readRetriesExhausted struct{ cause error }

func (e *readRetriesExhausted) Error() string { return e.cause.Error() }
func (e *readRetriesExhausted) Unwrap() error { return e.cause }

// RetryRemoteRead 只重试被限流的读取。平台提供的等待时长优先，取消即时生效。
func RetryRemoteRead[T any](ctx context.Context, read func(context.Context) (T, error)) (T, error) {
	var zero T
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		value, err := read(ctx)
		if err == nil {
			return value, nil
		}
		var exhausted *readRetriesExhausted
		var failure *domain.Error
		if errors.As(err, &exhausted) || !errors.As(err, &failure) || failure.Code != "RATE_LIMITED" {
			return zero, err
		}
		if attempt == 4 {
			return zero, &readRetriesExhausted{err}
		}
		delay := time.Second << attempt
		if hint := failure.RetryAfter; hint != nil && *hint > 0 {
			// 长冷却交给持久任务恢复，不能将它截短并提前请求平台。
			if *hint >= int(remoteSyncTimeout/time.Second) {
				return zero, &readRetriesExhausted{err}
			}
			delay = time.Duration(*hint) * time.Second
		}
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= delay {
			return zero, &readRetriesExhausted{err}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return zero, ctx.Err()
		case <-timer.C:
		}
	}
}
