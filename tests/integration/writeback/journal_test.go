package writeback_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
)

func TestJournalKeepsBlockedRecoveryAcrossReopen(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "journal")
	j, err := writeback.OpenJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	r := writeback.Record{ID: domain.NewID(), Owner: "owner", Stage: "prepared", RequestDigest: "request"}
	if err = j.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	r.Stage = "blocked"
	if err = j.Save(ctx, "prepared", r); err != nil {
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = writeback.OpenJournal(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	pending, err := j.Pending(ctx, "", 128)
	if err != nil || len(pending) != 1 || pending[0].ID != r.ID || pending[0].Stage != "blocked" {
		t.Fatalf("blocked journal lost after reopen: %+v %v", pending, err)
	}
}
