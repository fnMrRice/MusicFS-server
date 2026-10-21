package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

func assertIdentityPreflight(t *testing.T, root string) {
	t.Helper()
	want := errors.New("stop after identity preflight")
	driver := sqlite.Driver()
	driver.Name = "preflight-" + domain.NewID()
	driver.Open = func(context.Context, storage.Options) (storage.Backend, error) { return nil, want }
	if err := storage.Register(driver); err != nil {
		t.Fatal(err)
	}
	cfg := serverConfig(t)
	cfg.DataRoot, cfg.Storage.Backend = root, driver.Name
	err := bootstrap.Serve(t.Context(), cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if !errors.Is(err, want) {
		t.Fatalf("identity preflight blocked valid state: %v", err)
	}
}

func TestIdentityPreflightDoesNotCreateMissingJournal(t *testing.T) {
	root := t.TempDir()
	assertIdentityPreflight(t, root)
	if _, err := os.Stat(filepath.Join(root, "writeback")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("identity preflight created a journal directory: %v", err)
	}
}

func TestIdentityPreflightRetainsTerminalHistoryAndCurrentPendingWork(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "writeback")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for index, record := range []writeback.Record{
		{Target: writeback.Target{InstanceID: "legacy"}, Stage: "committed"},
		{Target: writeback.Target{InstanceID: "legacy"}, Stage: "aborted"},
		{Target: writeback.Target{InstanceID: instanceid.Canonical("current")}, Stage: "installed"},
	} {
		record.ID = strings.Repeat(string(rune('a'+index)), 48)
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, record.ID+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	assertIdentityPreflight(t, root)
}
