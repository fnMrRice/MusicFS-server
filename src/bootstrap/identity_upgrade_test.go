package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

func TestIdentityPreflightDoesNotCreateMissingJournal(t *testing.T) {
	root := t.TempDir()
	if err := checkLegacyWritebackJournals(context.Background(), root); err != nil {
		t.Fatal(err)
	}
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
	if err := checkLegacyWritebackJournals(context.Background(), root); err != nil {
		t.Fatalf("terminal legacy history or current pending work blocked startup: %v", err)
	}
}
