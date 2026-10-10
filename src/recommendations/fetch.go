package recommendations

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (s *Service) fetch(ctx context.Context, key cacheKey, r Runtime, e *entry, f *flight) {
	defer s.wg.Done()
	defer f.cancel()
	var page online.RecommendationPage
	var err error
	select {
	case s.slots <- struct{}{}:
		request, stop := context.WithTimeout(ctx, s.config.Timeout)
		if r.Provider == nil {
			err = domain.Fail("SOURCE_OFFLINE", "Recommendation source is offline")
		} else {
			page, err = r.Provider.PublicRecommendations(request, key.limit)
		}
		if err == nil {
			err = request.Err()
		}
		stop()
		<-s.slots
	case <-ctx.Done():
		err = ctx.Err()
	}
	var size int64
	if err == nil {
		err = online.ValidateRecommendations(r.Platform, page, key.limit)
		if err == nil && (page.ObservedAt.After(time.Now().Add(5*time.Second)) || !page.ExpiresAt.After(time.Now())) {
			err = domain.Fail("TEMPORARY_FAILURE", "Recommendation observation is expired or invalid")
		}
		if err == nil {
			encoded, e := json.Marshal(page)
			if e != nil || len(encoded) > pluginproto.MaxMessageBytes-4096 {
				err = domain.Fail("RATE_LIMITED", "Recommendation response exceeds its limit")
			} else {
				size = int64(len(encoded))
			}
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f.finished = true
	e.flight = nil
	defer close(f.done)
	if s.closed {
		f.result = failedSource(r, "SOURCE_OFFLINE", "Recommendation service is draining")
		return
	}
	if f.abandoned {
		f.result = s.snapshot(e, time.Now())
		return
	}
	now := time.Now().UTC()
	if err == nil {
		for s.bytes-e.bytes+size > s.config.MaxBytes {
			if !s.evict(e) {
				err = domain.Fail("RATE_LIMITED", "Recommendation cache byte budget is full")
				break
			}
		}
	}
	if err != nil {
		failure, cooldown := failureFor(err, s.config.FailureCooldown)
		if failure.Code == "PLATFORM_AUTH_REQUIRED" || failure.Code == "AUTH_REQUIRED" || failure.Code == "ACCESS_DENIED" {
			s.bytes -= e.bytes
			e.bytes = 0
			e.source = sourceFor(r)
		}
		e.source.Error = failure
		e.source.Stale = e.source.LastSuccessAt != nil
		e.retry = now.Add(cooldown)
		f.result = s.snapshot(e, now)
		return
	}
	expires := minTime(page.ExpiresAt, now.Add(s.config.TTL)).UTC()
	result := sourceFor(r)
	result.Items = page.Items
	result.LastSuccessAt = &now
	result.ExpiresAt = &expires
	e.source = cloneSource(result)
	e.expires = expires
	e.retry = time.Time{}
	s.bytes += size - e.bytes
	e.bytes = size
	f.result = cloneSource(e.source)
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func failureFor(err error, cooldown time.Duration) (*Failure, time.Duration) {
	code := "TEMPORARY_FAILURE"
	var retry *int
	var failure *domain.Error
	if errors.As(err, &failure) {
		code, retry = failure.Code, failure.RetryAfter
	} else {
		var rpc *pluginproto.RPCError
		if errors.As(err, &rpc) {
			code = rpc.Data.Code
			if ms := rpc.Data.RetryAfterMs; ms != nil && *ms > 0 && *ms <= 3600000 {
				n := int((*ms + 999) / 1000)
				retry = &n
			}
		}
	}
	switch code {
	case "AUTH_REQUIRED", "PLATFORM_AUTH_REQUIRED", "ACCESS_DENIED", "NOT_FOUND", "UNSUPPORTED", "SOURCE_OFFLINE", "RATE_LIMITED", "TEMPORARY_FAILURE", "PROTOCOL_MISMATCH", "CANCELED":
	default:
		code = "TEMPORARY_FAILURE"
	}
	if retry != nil && *retry > 0 && *retry <= 3600 {
		cooldown = max(cooldown, time.Duration(*retry)*time.Second)
	}
	n := int((cooldown + time.Second - 1) / time.Second)
	return &Failure{Code: code, Message: "Recommendation source is unavailable", RetryAfter: &n}, cooldown
}
