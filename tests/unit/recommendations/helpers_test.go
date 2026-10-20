package recommendations_test

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/recommendations"
)

type provider struct {
	calls atomic.Int32
	fn    func(context.Context, int, int32) (online.RecommendationPage, error)
}

func (p *provider) PublicRecommendations(ctx context.Context, limit int) (online.RecommendationPage, error) {
	n := p.calls.Add(1)
	if p.fn != nil {
		return p.fn(ctx, limit, n)
	}
	return page("42", 1), nil
}
func page(id string, count int) online.RecommendationPage {
	now := time.Now().UTC()
	p := online.RecommendationPage{Items: []online.Recommendation{}, ObservedAt: now, ExpiresAt: now.Add(2 * time.Minute)}
	for i := range count {
		trackCount := int64(3)
		pid := id + strconv.Itoa(i)
		p.Items = append(p.Items, online.Recommendation{Position: i, Reason: "Public selection", Playlist: online.Playlist{ExternalID: pid, Title: "Synthetic playlist", SourceURL: "https://music.163.com/playlist?id=" + pid, TrackCount: &trackCount}})
	}
	return p
}
func runtimeFor(owner, id string, p recommendations.Provider) recommendations.Runtime {
	return recommendations.Runtime{Owner: owner, InstanceID: id, Platform: online.Netease, ConfigRevision: "revision-1", Provider: p}
}
func service(t *testing.T, config recommendations.Config, runtimes ...recommendations.Runtime) *recommendations.Service {
	t.Helper()
	s, err := recommendations.New(runtimes, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func list(t *testing.T, s *recommendations.Service, owner string, request recommendations.Request) recommendations.Response {
	t.Helper()
	result, err := s.List(context.Background(), owner, request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func success(t *testing.T, source recommendations.Source) {
	t.Helper()
	if source.Error != nil || source.Stale || source.LastSuccessAt == nil || source.ExpiresAt == nil || source.Items == nil || source.Personalized || source.NextCursor != nil {
		t.Fatalf("invalid successful source: %+v", source)
	}
}
func expect(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}
func receive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not complete")
		var zero T
		return zero
	}
}
