package plugins_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
)

func TestRecommendationsActualWorkerCacheAndFailureState(t *testing.T) {
	path := onlineBinary(t, "./tests/testkit/online-worker", "online-fixture")
	cfg := onlineConfig(t, path, online.Netease)
	w := onlineStart(t, cfg)
	loginOnlineFixture(t, w)
	s, err := recommendations.New([]recommendations.Runtime{{Owner: cfg.UserID, InstanceID: cfg.InstanceID, Platform: cfg.Platform, ConfigRevision: "anonymous-v1", Provider: w}}, recommendations.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	request := recommendations.Request{Limit: 1}
	first, err := s.List(context.Background(), cfg.UserID, request)
	if err != nil || len(first.Sources) != 1 || first.Sources[0].Error != nil || len(first.Sources[0].Items) != 1 {
		t.Fatalf("first observation: %+v %v", first, err)
	}
	again, err := s.List(context.Background(), cfg.UserID, request)
	if err != nil || again.Sources[0].Error != nil || !again.Sources[0].LastSuccessAt.Equal(*first.Sources[0].LastSuccessAt) {
		t.Fatalf("cache missed and hit the worker rate limit: %+v %v", again, err)
	}
	request.Refresh = true
	stale, err := s.List(context.Background(), cfg.UserID, request)
	if err != nil || stale.Sources[0].Error == nil || stale.Sources[0].Error.Code != "RATE_LIMITED" || !stale.Sources[0].Stale || len(stale.Sources[0].Items) != 1 || !stale.Sources[0].LastSuccessAt.Equal(*first.Sources[0].LastSuccessAt) {
		t.Fatalf("worker failure lost prior data: %+v %v", stale, err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	again, err = s.List(context.Background(), cfg.UserID, request)
	if err != nil || again.Sources[0].Error.Code != "SOURCE_OFFLINE" || len(again.Sources[0].Items) != 0 {
		t.Fatal("explicit refresh ignored the shared cooldown")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.List(context.Background(), cfg.UserID, request)
	onlineExpect(t, err, "SOURCE_OFFLINE")
}
