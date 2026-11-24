package playlists_test

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func TestRemoteReadWaitsForPlatformCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		calls, seconds, started := 0, 7, time.Now()
		got, err := playlists.RetryRemoteRead(t.Context(), func(context.Context) (string, error) {
			calls++
			if calls == 1 {
				return "", &domain.Error{Code: "RATE_LIMITED", RetryAfter: &seconds}
			}
			return "track", nil
		})
		if err != nil || got != "track" || calls != 2 || time.Since(started) != 7*time.Second {
			t.Fatalf("unexpected retry result %q after %v, calls=%d, err=%v", got, time.Since(started), calls, err)
		}
	})
}

func TestRemoteReadExhaustedBatchDoesNotRestartObservation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		observations, reads, started := 0, 0, time.Now()
		cause := domain.Fail("RATE_LIMITED", "Too many requests")
		_, err := playlists.RetryRemoteRead(t.Context(), func(ctx context.Context) (int, error) {
			observations++
			return playlists.RetryRemoteRead(ctx, func(context.Context) (int, error) {
				reads++
				return 0, cause
			})
		})
		if !errors.Is(err, cause) || observations != 1 || reads != 5 || time.Since(started) != 15*time.Second {
			t.Fatalf("unexpected retry budget: observations=%d reads=%d duration=%v err=%v", observations, reads, time.Since(started), err)
		}
	})
}

func TestRemoteReadReturnsLongCooldownAndPermanentFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		seconds := 300
		for _, cause := range []error{
			&domain.Error{Code: "RATE_LIMITED", RetryAfter: &seconds},
			domain.Fail("RISK_CONTROL", "Request restricted"),
			domain.Fail("ACCESS_DENIED", "Access denied"),
		} {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			calls, started := 0, time.Now()
			_, err := playlists.RetryRemoteRead(ctx, func(context.Context) (int, error) {
				calls++
				return 0, cause
			})
			cancel()
			if !errors.Is(err, cause) || calls != 1 || time.Since(started) != 0 {
				t.Fatalf("unexpected retry for %v: calls=%d duration=%v err=%v", cause, calls, time.Since(started), err)
			}
		}
	})
}

func TestRemoteReadCancelsDuringCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan error, 1)
		calls, started := 0, time.Now()
		go func() {
			_, err := playlists.RetryRemoteRead(ctx, func(context.Context) (int, error) {
				calls++
				return 0, domain.Fail("RATE_LIMITED", "Too many requests")
			})
			done <- err
		}()
		synctest.Wait()
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) || calls != 1 || time.Since(started) != 0 {
			t.Fatalf("unexpected cancellation: calls=%d duration=%v err=%v", calls, time.Since(started), err)
		}
	})
}
