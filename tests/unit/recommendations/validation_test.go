package recommendations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/library/recommendations"
)

func TestDisplayNameFallbackDoesNotRejectValidRuntimeIdentity(t *testing.T) {
	for _, name := range []string{"", strings.Repeat("Long display name", 20), "line\nbreak"} {
		r := runtimeFor("owner", "stable-instance", &provider{})
		r.Name = name
		s := service(t, recommendations.Config{}, r)
		got := list(t, s, "owner", recommendations.Request{Limit: 1}).Sources[0]
		if got.Name != "NetEase Cloud Music" || got.InstanceID != "stable-instance" {
			t.Fatal("invalid display name did not fall back to the plugin alias")
		}
	}
}

func TestPublicRecommendationTimestampsAreUTC(t *testing.T) {
	p := &provider{fn: func(context.Context, int, int32) (online.RecommendationPage, error) {
		result := page("42", 1)
		result.ObservedAt = result.ObservedAt.In(time.FixedZone("source-zone", 8*60*60))
		result.ExpiresAt = result.ObservedAt.Add(2 * time.Minute)
		return result, nil
	}}
	s := service(t, recommendations.Config{}, runtimeFor("owner", "instance", p))
	result := list(t, s, "owner", recommendations.Request{Limit: 1}).Sources[0]
	if result.LastSuccessAt == nil || result.ExpiresAt == nil || !strings.HasSuffix(result.LastSuccessAt.Format(time.RFC3339Nano), "Z") || !strings.HasSuffix(result.ExpiresAt.Format(time.RFC3339Nano), "Z") {
		t.Fatal("public recommendation timestamps were not normalized to UTC")
	}
}

func TestInvalidProviderObservationsAreNotCachedAsSuccess(t *testing.T) {
	cases := map[string]func(*online.RecommendationPage){
		"personalized": func(p *online.RecommendationPage) { p.Personalized = true },
		"pagination":   func(p *online.RecommendationPage) { cursor := "invented"; p.NextCursor = &cursor },
		"source":       func(p *online.RecommendationPage) { p.Items[0].Playlist.SourceURL = "https://evil.example/playlist" },
		"order":        func(p *online.RecommendationPage) { p.Items[0].Position = 2 },
		"expired": func(p *online.RecommendationPage) {
			p.ObservedAt = time.Now().Add(-3 * time.Minute)
			p.ExpiresAt = p.ObservedAt.Add(2 * time.Minute)
		},
		"future": func(p *online.RecommendationPage) {
			p.ObservedAt = time.Now().Add(time.Hour)
			p.ExpiresAt = p.ObservedAt.Add(2 * time.Minute)
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := &provider{fn: func(context.Context, int, int32) (online.RecommendationPage, error) {
				result := page("42", 1)
				mutate(&result)
				return result, nil
			}}
			s := service(t, recommendations.Config{}, runtimeFor("owner", "instance", p))
			got := list(t, s, "owner", recommendations.Request{Limit: 1}).Sources[0]
			if got.Error == nil || got.LastSuccessAt != nil || len(got.Items) != 0 {
				t.Fatal("invalid provider observation was published")
			}
		})
	}
}

func TestConfigurationAndRequestBoundaries(t *testing.T) {
	r := runtimeFor("owner", "instance", &provider{})
	account := r
	account.AccountID = "private"
	_, err := recommendations.New([]recommendations.Runtime{account}, recommendations.Config{})
	expect(t, err, "UNSUPPORTED")
	_, err = recommendations.New([]recommendations.Runtime{r, r}, recommendations.Config{})
	expect(t, err, "INVALID_ARGUMENT")
	for _, config := range []recommendations.Config{{MaxEntries: 257}, {MaxBytes: 128}, {MaxConcurrent: 5}, {TTL: 3 * time.Minute}, {Timeout: time.Minute}, {FailureCooldown: 2 * time.Minute}} {
		_, err = recommendations.New([]recommendations.Runtime{r}, config)
		expect(t, err, "INVALID_ARGUMENT")
	}
	s := service(t, recommendations.Config{}, r)
	for _, request := range []recommendations.Request{{Limit: 0}, {Limit: 51}, {Limit: 1, InstanceIDs: []string{"instance", "instance"}}} {
		_, err = s.List(context.Background(), "owner", request)
		expect(t, err, "INVALID_ARGUMENT")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = s.List(context.Background(), "owner", recommendations.Request{Limit: 1})
	expect(t, err, "SOURCE_OFFLINE")
}
