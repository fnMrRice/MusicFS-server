package works_test

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestWorkRejectsInvalidMetadataWithoutWrites(t *testing.T) {
	store, _ := workStore(t)
	for _, test := range []struct {
		name   string
		mutate func(*works.CreateRequest)
	}{
		{"missing-type", func(r *works.CreateRequest) { r.ContentType = "" }},
		{"type-format", func(r *works.CreateRequest) { r.ContentType = "custom.type" }},
		{"empty-title", func(r *works.CreateRequest) { r.Title = " " }},
		{"long-title", func(r *works.CreateRequest) { r.Title = strings.Repeat("界", 257) }},
		{"invalid-utf8", func(r *works.CreateRequest) { r.Title = string([]byte{0xff}) }},
		{"control", func(r *works.CreateRequest) { r.Description = "invalid\x00text" }},
		{"long-description", func(r *works.CreateRequest) { r.Description = strings.Repeat("x", works.MaxDescriptionLength+1) }},
		{"namespace", func(r *works.CreateRequest) { r.ExternalIDs[0].Namespace = "Bad" }},
		{"empty-external-id", func(r *works.CreateRequest) { r.ExternalIDs[0].Value = " " }},
		{"duplicate-external-id", func(r *works.CreateRequest) { r.ExternalIDs = append(r.ExternalIDs, r.ExternalIDs[0]) }},
		{"external-limit", func(r *works.CreateRequest) { r.ExternalIDs = make([]works.ExternalID, works.MaxExternalIDs+1) }},
		{"credit-role", func(r *works.CreateRequest) { r.Credits[0].Role = "../" }},
		{"empty-credit", func(r *works.CreateRequest) { r.Credits[0].Name = "" }},
		{"duplicate-credit", func(r *works.CreateRequest) { r.Credits = append(r.Credits, r.Credits[0]) }},
		{"credit-limit", func(r *works.CreateRequest) { r.Credits = make([]works.Credit, works.MaxCredits+1) }},
		{"duplicate-tag", func(r *works.CreateRequest) { r.Tags = []string{"one", " one "} }},
		{"empty-tag", func(r *works.CreateRequest) { r.Tags = []string{""} }},
		{"tag-limit", func(r *works.CreateRequest) { r.Tags = make([]string, works.MaxTags+1) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := sampleRequest()
			test.mutate(&request)
			_, err := store.CreateWork(t.Context(), "alice", request, domain.NewID())
			requireCode(t, err, "INVALID_ARGUMENT")
		})
	}
	for _, owner := range []string{"", "bad owner", strings.Repeat("x", 129)} {
		_, err := store.CreateWork(t.Context(), owner, sampleRequest(), "valid-key")
		requireCode(t, err, "INVALID_ARGUMENT")
	}
	for _, key := range []string{"short", "invalid key", strings.Repeat("x", 129)} {
		_, err := store.CreateWork(t.Context(), "alice", sampleRequest(), key)
		requireCode(t, err, "INVALID_ARGUMENT")
	}
	got, err := store.Works(t.Context(), "alice", works.Query{Limit: 101})
	if err != nil || len(got) != 0 {
		t.Fatal("invalid creation left a work", err)
	}
	created := createWork(t, store, "alice", sampleRequest())
	for _, revision := range []int64{0, -1, math.MaxInt64} {
		_, err = store.UpdateWork(t.Context(), "alice", created.ID, works.UpdateRequest{ExpectedRevision: revision, Title: "No write"}, domain.NewID())
		requireCode(t, err, "INVALID_ARGUMENT")
	}
	unchanged, err := store.Work(t.Context(), "alice", created.ID)
	if err != nil || !reflect.DeepEqual(created, unchanged) {
		t.Fatal("invalid update changed work", err)
	}
}

func TestWorkQueryRejectsUnboundedOrMalformedInput(t *testing.T) {
	store, _ := workStore(t)
	for _, query := range []works.Query{{Limit: 0}, {Limit: 102}, {Limit: -1}, {Limit: 1, After: "bad id"}, {Limit: 1, ContentType: "INVALID"}, {Limit: 1, Q: strings.Repeat("x", 257)}, {Limit: 1, Tag: "bad\x00tag"}, {Limit: 1, Credit: strings.Repeat("x", 257)}} {
		_, err := store.Works(t.Context(), "alice", query)
		requireCode(t, err, "INVALID_ARGUMENT")
	}
}
