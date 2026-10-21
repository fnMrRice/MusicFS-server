package recommendations_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

func TestConcurrentWaitersShareFetchAndCancelIndependently(t *testing.T) {
	started, finish, canceled := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	p := &provider{fn: func(ctx context.Context, _ int, n int32) (online.RecommendationPage, error) {
		if n == 1 {
			close(started)
		}
		select {
		case <-finish:
			return page("42", 1), nil
		case <-ctx.Done():
			canceled <- struct{}{}
			return online.RecommendationPage{}, ctx.Err()
		}
	}}
	s := service(t, recommendations.Config{}, runtimeFor("owner", "instance", p))
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := s.List(ctx, "owner", recommendations.Request{Limit: 1}); first <- err }()
	receive(t, started)
	results := make(chan error, 16)
	for range 16 {
		go func() {
			r, err := s.List(context.Background(), "owner", recommendations.Request{Limit: 1})
			if err == nil && r.Sources[0].Error != nil {
				err = errors.New(r.Sources[0].Error.Code)
			}
			results <- err
		}()
	}
	// Allow the other callers to enter the shared wait before canceling its creator.
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := receive(t, first); !errors.Is(err, context.Canceled) {
		t.Fatalf("creator cancellation: %v", err)
	}
	select {
	case <-canceled:
		t.Fatal("one canceled waiter stopped other callers")
	case <-time.After(20 * time.Millisecond):
	}
	close(finish)
	for range 16 {
		if err := receive(t, results); err != nil {
			t.Fatal(err)
		}
	}
	if p.calls.Load() != 1 {
		t.Fatal("concurrent requests were not merged")
	}
}

func TestAbandonedFetchDoesNotPoisonTheNextCaller(t *testing.T) {
	started, canceled, finish := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := &provider{fn: func(ctx context.Context, _ int, n int32) (online.RecommendationPage, error) {
		if n == 1 {
			close(started)
			<-ctx.Done()
			close(canceled)
			<-finish
			return online.RecommendationPage{}, ctx.Err()
		}
		return page("99", 1), nil
	}}
	s := service(t, recommendations.Config{}, runtimeFor("owner", "instance", p))
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := s.List(ctx, "owner", recommendations.Request{Limit: 1}); first <- err }()
	receive(t, started)
	cancel()
	receive(t, canceled)
	if err := receive(t, first); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	second := make(chan recommendations.Response, 1)
	go func() { r, _ := s.List(context.Background(), "owner", recommendations.Request{Limit: 1}); second <- r }()
	close(finish)
	r := receive(t, second)
	if len(r.Sources) != 1 {
		t.Fatal("next caller did not complete")
	}
	success(t, r.Sources[0])
	if p.calls.Load() != 2 {
		t.Fatal("next caller inherited an abandoned result")
	}
}

func TestConcurrencyLimitAndShutdownDrain(t *testing.T) {
	started := make(chan struct{}, 3)
	var active, maximum, stopped atomic.Int32
	fn := func(ctx context.Context, _ int, _ int32) (online.RecommendationPage, error) {
		n := active.Add(1)
		for old := maximum.Load(); n > old && !maximum.CompareAndSwap(old, n); old = maximum.Load() {
		}
		started <- struct{}{}
		<-ctx.Done()
		active.Add(-1)
		stopped.Add(1)
		return online.RecommendationPage{}, ctx.Err()
	}
	s := service(t, recommendations.Config{MaxConcurrent: 2}, runtimeFor("owner", "a", &provider{fn: fn}), runtimeFor("owner", "b", &provider{fn: fn}), runtimeFor("owner", "c", &provider{fn: fn}))
	done := make(chan error, 1)
	go func() {
		_, err := s.List(context.Background(), "owner", recommendations.Request{Limit: 1})
		done <- err
	}()
	receive(t, started)
	receive(t, started)
	select {
	case <-started:
		t.Fatal("global concurrency limit was exceeded")
	case <-time.After(20 * time.Millisecond):
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if maximum.Load() > 2 || active.Load() != 0 || stopped.Load() < 2 {
		t.Fatal("shutdown returned with active fetches")
	}
	if err := receive(t, done); err != nil {
		expect(t, err, "SOURCE_OFFLINE")
	}
}

func TestTimeoutAndBusyCacheRemainBounded(t *testing.T) {
	started := make(chan struct{}, 1)
	p := &provider{fn: func(ctx context.Context, _ int, _ int32) (online.RecommendationPage, error) {
		started <- struct{}{}
		<-ctx.Done()
		return online.RecommendationPage{}, ctx.Err()
	}}
	other := &provider{}
	s := service(t, recommendations.Config{MaxEntries: 1, Timeout: 100 * time.Millisecond}, runtimeFor("owner", "slow", p), runtimeFor("owner", "other", other))
	done := make(chan recommendations.Response, 1)
	go func() {
		r, _ := s.List(context.Background(), "owner", recommendations.Request{InstanceIDs: []string{"slow"}, Limit: 1})
		done <- r
	}()
	receive(t, started)
	r := list(t, s, "owner", recommendations.Request{InstanceIDs: []string{"other"}, Limit: 1})
	if r.Sources[0].Error == nil || r.Sources[0].Error.Code != "RATE_LIMITED" || other.calls.Load() != 0 {
		t.Fatal("busy cache admitted an unbounded fetch")
	}
	r = receive(t, done)
	if len(r.Sources) != 1 || r.Sources[0].Error == nil || r.Sources[0].LastSuccessAt != nil {
		t.Fatal("timed out request became successful")
	}
}
