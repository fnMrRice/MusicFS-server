package recommendations_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
)

func TestRuntimeReplacementInvalidatesOnlyChangedInstanceAndBlocksDisabledCache(t *testing.T) {
	a, b := &provider{}, &provider{}
	gate := plugincontrol.NewGate(true)
	r := runtimeFor("owner", "a", a)
	r.Gate = gate
	s := service(t, recommendations.Config{}, r, runtimeFor("owner", "b", b))
	request := recommendations.Request{Limit: 1}
	list(t, s, "owner", request)
	gate.SetAccepting(false, false)
	blocked := list(t, s, "owner", request)
	if blocked.Sources[0].Error == nil || blocked.Sources[0].Error.Code != "PLUGIN_DISABLED" || len(blocked.Sources[0].Items) != 0 {
		t.Fatal("disabled cache was exposed as ready", blocked)
	}
	newProvider := &provider{fn: func(context.Context, int, int32) (online.RecommendationPage, error) { return page("99", 1), nil }}
	r.Provider = newProvider
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	gate.SetAccepting(true, false)
	updated := list(t, s, "owner", request)
	if updated.Sources[0].Items[0].Playlist.ExternalID != "990" || a.calls.Load() != 1 || newProvider.calls.Load() != 1 || b.calls.Load() != 1 {
		t.Fatal("replacement cache isolation failed", updated)
	}
}

func TestRecommendationSnapshotCannotBorrowReplacementGate(t *testing.T) {
	p := &provider{}
	gate := plugincontrol.NewGate(true)
	r := runtimeFor("owner", "a", p)
	r.Gate = gate
	s := service(t, recommendations.Config{}, r)
	list(t, s, "owner", recommendations.Request{Limit: 1})
	gate.SetAccepting(false, false)
	gate.Rotate()
	gate.SetAccepting(true, false)
	old := list(t, s, "owner", recommendations.Request{Limit: 1})
	if old.Sources[0].Error == nil || old.Sources[0].Error.Code != "RESOURCE_CHANGED" || len(old.Sources[0].Items) != 0 || p.calls.Load() != 1 {
		t.Fatal("old cache or runtime borrowed replacement admission", old)
	}
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	current := list(t, s, "owner", recommendations.Request{Limit: 1})
	if current.Sources[0].Error != nil || p.calls.Load() != 2 {
		t.Fatal("replacement runtime was not admitted", current)
	}
}

func TestSharedRecommendationFetchHoldsLeaseUntilCleanup(t *testing.T) {
	entered, canceled, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	p := &provider{fn: func(ctx context.Context, _ int, _ int32) (online.RecommendationPage, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-cleaned
		return online.RecommendationPage{}, ctx.Err()
	}}
	gate := plugincontrol.NewGate(true)
	r := runtimeFor("owner", "a", p)
	r.Gate = gate
	s := service(t, recommendations.Config{}, r)
	done := make(chan struct{})
	go func() {
		_, _ = s.List(context.Background(), "owner", recommendations.Request{Limit: 1})
		close(done)
	}()
	<-entered
	gate.SetAccepting(false, true)
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("force did not cancel recommendation fetch")
	}
	if gate.Active() == 0 {
		t.Fatal("admitted fetch lease disappeared before provider cleanup")
	}
	close(cleaned)
	<-done
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if gate.Active() != 0 {
		t.Fatal("recommendation leases leaked after cleanup")
	}
}

func TestLateRecommendationResultCannotRepublishRetiredGeneration(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	p := &provider{fn: func(context.Context, int, int32) (online.RecommendationPage, error) {
		close(entered)
		<-finish
		return page("42", 1), nil
	}}
	r := runtimeFor("owner", "a", p)
	s := service(t, recommendations.Config{}, r)
	done := make(chan recommendations.Response, 1)
	go func() {
		result, _ := s.List(context.Background(), "owner", recommendations.Request{Limit: 1})
		done <- result
	}()
	<-entered
	r.Provider = &provider{fn: func(context.Context, int, int32) (online.RecommendationPage, error) { return page("99", 1), nil }}
	if err := s.UpdateRuntime(r); err != nil {
		t.Fatal(err)
	}
	close(finish)
	old := receive(t, done)
	if old.Sources[0].Error == nil || old.Sources[0].Error.Code != "RESOURCE_CHANGED" {
		t.Fatal("late result was published", old)
	}
	current := list(t, s, "owner", recommendations.Request{Limit: 1})
	if current.Sources[0].Items[0].Playlist.ExternalID != "990" {
		t.Fatal("retired cache resurrected", current)
	}
}
