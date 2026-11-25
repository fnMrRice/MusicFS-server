package storage_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/fnMrRice/MusicFS-server/plugins/database/postgres"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestPostgresExclusiveStoreAndRestart(t *testing.T) {
	values := testkit.PostgresOptions(t)
	if file := values["passwordFile"]; file != "" {
		password, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv("MUSICFS_TEST_INSTANCE_PASSWORD", string(password))
		delete(values, "passwordFile")
		values["passwordEnv"] = "MUSICFS_TEST_INSTANCE_PASSWORD"
	}
	options := storage.Options{DataRoot: t.TempDir(), Values: values}
	open := func() storage.Backend {
		t.Helper()
		s, err := storage.Open(t.Context(), "postgres", options)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { s.Close() })
		return s
	}
	s := open()
	checkSessionRenewal(t, s)
	other := storage.Options{DataRoot: t.TempDir(), Values: values}
	if duplicate, err := storage.Open(t.Context(), "postgres", other); err == nil {
		duplicate.Close()
		t.Fatal("two servers acquired the same PostgreSQL database")
	}
	if m, err := storage.OpenMaintenance(t.Context(), "postgres", other); err == nil {
		m.Close()
		t.Fatal("offline maintenance opened an active PostgreSQL database")
	}
	session := auth.Session{ID: "persisted", User: auth.User{ID: "admin", Role: "admin"}, CSRFToken: "csrf", ExpiresAt: time.Now().UTC().Add(time.Hour)}
	if err := s.SaveAuth(t.Context(), session); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open()
	value, err := s.FindAuth(t.Context(), session.ID)
	if err != nil || value.User.ID != session.User.ID {
		t.Fatalf("session missing after PostgreSQL restart: %v", err)
	}
	if _, err := os.Stat(filepath.Join(options.DataRoot, "musicfs.sqlite")); !os.IsNotExist(err) {
		t.Fatalf("unexpected local database: %v", err)
	}
}

func TestPostgresLibraryOperationQueueAndMergeReceipt(t *testing.T) {
	ctx := t.Context()
	store, err := storage.Open(ctx, "postgres", storage.Options{DataRoot: t.TempDir(), Values: testkit.PostgresOptions(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	checkMixedLibraryOperationQueue(t, store)
	req := matching.BulkMergeRequest{Mode: "title_artist"}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	merge := enqueueLibraryOperation(t, store, "admin", tasks.BulkMerge, "recovered-merge", string(raw))
	op, err := store.ClaimOperation(ctx)
	if err != nil || op == nil || op.Task.ID != merge.ID {
		t.Fatal(op, err)
	}
	if _, err := store.BulkMergeSummary(ctx, "admin", req, merge.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RecoverOperations(ctx); err != nil {
		t.Fatal(err)
	}
	completed, err := store.GetRefreshTask(ctx, "admin", merge.ID)
	if err != nil || completed.Status != "succeeded" || completed.Error != nil {
		t.Fatal("could not recover committed merge", completed, err)
	}
}
