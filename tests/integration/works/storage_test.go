package works_test

import (
	"errors"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func workStore(t *testing.T) (*sqlite.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "works.sqlite")
	store, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, path
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func sampleRequest() works.CreateRequest {
	return works.CreateRequest{ContentType: "asmr", Title: "雨夜", Description: "安静的雨声\n含无背景音乐版本", ExternalIDs: []works.ExternalID{{Namespace: "dlsite", Value: "RJ123456"}}, Credits: []works.Credit{{Role: "circle", Name: "雨声社"}, {Role: "voice", Name: "小雨"}}, Tags: []string{"雨声", "耳语"}}
}

func createWork(t *testing.T, store *sqlite.Store, owner string, request works.CreateRequest) works.Work {
	t.Helper()
	result, err := store.CreateWork(t.Context(), owner, request, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWorkIdentityPersistenceIsolationAndIdempotency(t *testing.T) {
	store, path := workStore(t)
	request := sampleRequest()
	request.Title = " 雨夜 "
	first, err := store.CreateWork(t.Context(), "alice", request, "create-once")
	if err != nil {
		t.Fatal(err)
	}
	request.Title = "雨夜"
	replay, err := store.CreateWork(t.Context(), "alice", request, "create-once")
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatal("canonical replay changed the work", err)
	}
	second := createWork(t, store, "alice", request)
	if first.ID == second.ID || first.Revision != 1 || first.CreatedAt.IsZero() || first.UpdatedAt != first.CreatedAt {
		t.Fatal("work identity or initial revision is invalid")
	}
	request.Title = "Different"
	_, err = store.CreateWork(t.Context(), "alice", request, "create-once")
	requireCode(t, err, "CONFLICT")
	foreign, err := store.CreateWork(t.Context(), "bob", request, "create-once")
	if err != nil || foreign.ID == first.ID {
		t.Fatal("idempotency key crossed owners", err)
	}
	_, err = store.Work(t.Context(), "bob", first.ID)
	requireCode(t, err, "NOT_FOUND")
	_, err = store.UpdateWork(t.Context(), "bob", first.ID, works.UpdateRequest{ExpectedRevision: 1, Title: "Denied"}, "update-foreign")
	requireCode(t, err, "NOT_FOUND")
	listed, err := store.Works(t.Context(), "bob", works.Query{Limit: 101})
	if err != nil || len(listed) != 1 || listed[0].ID != foreign.ID {
		t.Fatal("work list crossed owners", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	actual, err := reopened.Work(t.Context(), "alice", first.ID)
	if err != nil || !reflect.DeepEqual(actual, first) {
		t.Fatal("work metadata did not survive reopening", err)
	}
	replay, err = reopened.CreateWork(t.Context(), "alice", sampleRequest(), "create-once")
	if err != nil || !reflect.DeepEqual(replay, first) {
		t.Fatal("idempotency did not survive reopening", err)
	}
}

func TestWorkRevisionReplayAndAtomicMetadataReplacement(t *testing.T) {
	store, _ := workStore(t)
	first := createWork(t, store, "alice", sampleRequest())
	request := works.UpdateRequest{ExpectedRevision: 1, Title: "晴日", Description: "新的简介", ExternalIDs: []works.ExternalID{{Namespace: "local", Value: "SUN-1"}}, Credits: []works.Credit{{Role: "voice", Name: "小晴"}}, Tags: []string{"鸟鸣"}}
	updated, err := store.UpdateWork(t.Context(), "alice", first.ID, request, "update-once")
	if err != nil || updated.Revision != 2 || updated.ContentType != first.ContentType || updated.CreatedAt != first.CreatedAt {
		t.Fatal("work update failed", err)
	}
	replay, err := store.UpdateWork(t.Context(), "alice", first.ID, request, "update-once")
	if err != nil || !reflect.DeepEqual(replay, updated) {
		t.Fatal("update replay changed the work", err)
	}
	request.Title = "Stale"
	_, err = store.UpdateWork(t.Context(), "alice", first.ID, request, "update-once")
	requireCode(t, err, "CONFLICT")
	_, err = store.UpdateWork(t.Context(), "alice", first.ID, request, "stale-update")
	requireCode(t, err, "CONFLICT")
	unchanged, err := store.Work(t.Context(), "alice", first.ID)
	if err != nil || !reflect.DeepEqual(unchanged, updated) {
		t.Fatal("failed update changed metadata", err)
	}
	for _, query := range []works.Query{{Q: "RJ123456"}, {Tag: "雨声"}, {Credit: "小雨"}} {
		query.Limit = 101
		got, err := store.Works(t.Context(), "alice", query)
		if err != nil || len(got) != 0 {
			t.Fatal("replaced metadata remains searchable", query, err)
		}
	}
	got, err := store.Works(t.Context(), "alice", works.Query{Q: "SUN-1", Tag: "鸟鸣", Credit: "小晴", Limit: 101})
	if err != nil || len(got) != 1 || got[0].ID != first.ID {
		t.Fatal("updated metadata is not searchable", err)
	}
}

func TestWorkConcurrentRevisionAllowsOneWriter(t *testing.T) {
	store, _ := workStore(t)
	first := createWork(t, store, "alice", sampleRequest())
	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, title := range []string{"First", "Second"} {
		wait.Add(1)
		go func(title string) {
			defer wait.Done()
			_, err := store.UpdateWork(t.Context(), "alice", first.ID, works.UpdateRequest{ExpectedRevision: 1, Title: title}, domain.NewID())
			results <- err
		}(title)
	}
	wait.Wait()
	close(results)
	succeeded := 0
	for err := range results {
		if err == nil {
			succeeded++
		} else {
			requireCode(t, err, "CONFLICT")
		}
	}
	current, err := store.Work(t.Context(), "alice", first.ID)
	if succeeded != 1 || err != nil || current.Revision != 2 {
		t.Fatal("concurrent writers lost revision protection", succeeded, err)
	}
}
