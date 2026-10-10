package onlinehost_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/onlinehost"
)

type blocked struct {
	onlinehost.Provider
	entered chan struct{}
}

func (b *blocked) Search(ctx context.Context, _ online.SearchRequest) (online.SearchPage, error) {
	b.entered <- struct{}{}
	<-ctx.Done()
	return online.SearchPage{}, ctx.Err()
}

func TestOnlineOwnerCapacityAndCancellation(t *testing.T) {
	p := &blocked{entered: make(chan struct{}, 4)}
	s, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "public", Name: "Public", Platform: online.Netease, ConfigRevision: "revision", Enabled: true, Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request := online.SearchRequest{Query: "Synthetic", Limit: 1}
	results := make(chan error, 4)
	for range 4 {
		go func() { _, err := s.Search(ctx, "owner", "public", request); results <- err }()
	}
	for range 4 {
		select {
		case <-p.entered:
		case <-ctx.Done():
			t.Fatal("requests were not admitted")
		}
	}
	_, err = s.Search(ctx, "owner", "public", request)
	var detail *domain.Error
	if !errors.As(err, &detail) || detail.Code != "RATE_LIMITED" || detail.RetryAfter == nil || *detail.RetryAfter != 1 {
		t.Fatal("online capacity was not bounded", err)
	}
	if _, err := s.Runtime(ctx, "other", "public", ""); !errors.As(err, &detail) || detail.Code != "NOT_FOUND" {
		t.Fatal("owner bypass", err)
	}
	if _, err := s.Runtime(ctx, "owner", "public", "account"); !errors.As(err, &detail) || detail.Code != "UNSUPPORTED" {
		t.Fatal("account accepted", err)
	}
	cancel()
	for range 4 {
		if err := <-results; !errors.Is(err, context.Canceled) {
			t.Fatal("cancel was not propagated", err)
		}
	}
}

func TestOnlineTypedNilAndPrivateDescriptors(t *testing.T) {
	var p *blocked
	s, err := onlinehost.New([]onlinehost.Runtime{{Owner: "mine", InstanceID: "nil", Platform: online.Netease, ConfigRevision: "rev", Enabled: true, Provider: p}, {Owner: "other", InstanceID: "secret", Platform: online.AppleMusic, ConfigRevision: "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.Instances(context.Background(), "mine")
	if err != nil || len(items) != 1 || items[0].Availability != "offline" || items[0].AccountID != "" {
		t.Fatal("invalid offline projection", err)
	}
	_, err = s.Runtime(context.Background(), "mine", "nil", "")
	var detail *domain.Error
	if !errors.As(err, &detail) || detail.Code != "SOURCE_OFFLINE" {
		t.Fatal("typed nil provider accepted", err)
	}
}
