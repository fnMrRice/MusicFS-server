package online_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
)

func TestSearchCursorContinuesBoundedObservation(t *testing.T) {
	var calls atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 1 {
			return response(neSearch).RoundTrip(r)
		}
		if r.URL.Query().Get("offset") != "1" {
			t.Error("cursor offset was not preserved")
		}
		return response(`{"code":200,"result":{"songs":[{"id":2,"name":"Second","artists":[],"album":{},"duration":1}],"songCount":2,"hasMore":false}}`).RoundTrip(r)
	}))
	r := online.SearchRequest{Query: "synthetic", Limit: 1}
	first, err := c.Search(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	r.Cursor = *first.NextCursor
	time.Sleep(1010 * time.Millisecond)
	last, err := c.Search(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if last.NextCursor != nil || last.Truncated || len(last.Tracks) != 1 || last.Tracks[0].ExternalID != "2" {
		t.Fatalf("bad last page: %+v", last)
	}
}

func TestConcurrentRequestsShareClientAdmission(t *testing.T) {
	var calls, successes atomic.Int32
	c := scoped(t, online.Netease, roundTrip(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return response(neSearch).RoundTrip(r)
	}))
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Go(func() {
			_, err := c.Search(context.Background(), online.SearchRequest{Query: "synthetic", Limit: 1})
			if err == nil {
				successes.Add(1)
				return
			}
			var typed *domain.Error
			if !errors.As(err, &typed) || typed.Code != "RATE_LIMITED" {
				t.Errorf("unexpected admission result: %v", err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 || successes.Load() != 1 {
		t.Fatalf("concurrent requests bypassed admission: %d/%d", calls.Load(), successes.Load())
	}
}

func TestBusinessRateLimitRetainsCooldown(t *testing.T) {
	c := scoped(t, online.Netease, response(`{"code":503,"message":"ignored"}`))
	for i := 0; i < 2; i++ {
		_, err := c.Search(context.Background(), online.SearchRequest{Query: "test", Limit: 1})
		expectCode(t, err, "RATE_LIMITED")
		if err.(*domain.Error).RetryAfter == nil || *err.(*domain.Error).RetryAfter < 59 {
			t.Fatal("business cooldown was ignored")
		}
	}
}
