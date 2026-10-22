package works_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func TestWorkQueryIntersectionsLiteralSearchAndKeysetPagination(t *testing.T) {
	store, _ := workStore(t)
	first := createWork(t, store, "alice", sampleRequest())
	secondRequest := sampleRequest()
	secondRequest.ContentType, secondRequest.Title = "music", "雨夜"
	secondRequest.ExternalIDs = nil
	second := createWork(t, store, "alice", secondRequest)
	third := createWork(t, store, "alice", works.CreateRequest{ContentType: "asmr", Title: "100%_静寂", Tags: []string{"雨声集"}, Credits: []works.Credit{{Role: "voice", Name: "小雨儿"}}})
	createWork(t, store, "bob", sampleRequest())
	for _, test := range []struct {
		name  string
		query works.Query
		want  []string
	}{
		{"all", works.Query{}, []string{first.ID, second.ID, third.ID}},
		{"type", works.Query{ContentType: "asmr"}, []string{first.ID, third.ID}},
		{"title", works.Query{Q: "雨夜"}, []string{first.ID, second.ID}},
		{"description", works.Query{Q: "背景音乐"}, []string{first.ID, second.ID}},
		{"external-id", works.Query{Q: "rj123"}, []string{first.ID}},
		{"namespace", works.Query{Q: "DLSITE"}, []string{first.ID}},
		{"credit-search", works.Query{Q: "小雨"}, []string{first.ID, second.ID, third.ID}},
		{"tag-exact", works.Query{Tag: "雨声"}, []string{first.ID, second.ID}},
		{"credit-exact", works.Query{Credit: "小雨"}, []string{first.ID, second.ID}},
		{"intersection", works.Query{ContentType: "asmr", Q: "rj123", Tag: "雨声", Credit: "小雨"}, []string{first.ID}},
		{"disjoint", works.Query{ContentType: "music", Q: "rj123"}, []string{}},
		{"literal-wildcards", works.Query{Q: "%_"}, []string{third.ID}},
		{"sql-text", works.Query{Q: "' OR 1=1 --"}, []string{}},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.query.Limit = 101
			got, err := store.Works(t.Context(), "alice", test.query)
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, item := range got {
				ids = append(ids, item.ID)
			}
			sort.Strings(test.want)
			if !reflect.DeepEqual(ids, test.want) {
				t.Fatalf("work IDs = %v, want %v", ids, test.want)
			}
		})
	}
	want := []string{first.ID, second.ID, third.ID}
	sort.Strings(want)
	after := ""
	for _, id := range want {
		page, err := store.Works(t.Context(), "alice", works.Query{After: after, Limit: 1})
		if err != nil || len(page) != 1 || page[0].ID != id {
			t.Fatal("keyset page skipped or duplicated a work", page, err)
		}
		after = id
	}
	page, err := store.Works(t.Context(), "alice", works.Query{After: after, Limit: 1})
	if err != nil || page == nil || len(page) != 0 {
		t.Fatal("empty page must be an array", err)
	}
}

func TestStoredWorkDoesNotDependOnContentPluginRegistration(t *testing.T) {
	store, _ := workStore(t)
	created := createWork(t, store, "alice", works.CreateRequest{ContentType: "removed_plugin_type", Title: "Archived work"})
	if created.ExternalIDs == nil || created.Credits == nil || created.Tags == nil {
		t.Fatal("empty metadata must be arrays")
	}
	read, err := store.Work(t.Context(), "alice", created.ID)
	if err != nil || !reflect.DeepEqual(created, read) {
		t.Fatal("stored work became unreadable without a plugin", err)
	}
	listed, err := store.Works(t.Context(), "alice", works.Query{ContentType: created.ContentType, Limit: 1})
	if err != nil || len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatal("unregistered content type is not queryable", err)
	}
}
