package storage_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func TestWritebackRecoveryRetainsSelectionFenceAfterDatabaseReopen(t *testing.T) {
	f := newWritebackFixture(t)
	ctx := context.Background()
	j := f.queue(t)
	if err := f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 2); err != nil {
		t.Fatal(err)
	}
	if err := f.s.RecoverWritebackQueue(ctx, nil); err != nil {
		t.Fatal(err)
	}
	queued, err := f.s.GetWriteback(ctx, "admin", j.ID)
	if err != nil || queued.Status != "queued" {
		t.Fatal("no-journal task did not requeue", err)
	}
	if err = f.s.BeginWriteback(ctx, "admin", j.ID, f.target, 2); err != nil {
		t.Fatal(err)
	}
	var sequence int
	var name, path string
	if err = f.s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Close(); err != nil {
		t.Fatal(err)
	}
	f.s, err = sqlite.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.s.Close()
	if err = f.s.RecoverWritebackQueue(ctx, map[string]bool{j.ID: true}); err != nil {
		t.Fatal(err)
	}
	recovered, err := f.s.GetWriteback(ctx, "admin", j.ID)
	if err != nil || recovered.Status != "recovery" {
		t.Fatal("journal task was not recoverable", err)
	}
	if _, err = f.s.CancelWriteback(ctx, "admin", j.ID); err == nil {
		t.Fatal("unrecovered writeback was cancelled")
	}
	_, err = f.s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: f.track.ID, ExpectedRevision: 2, Field: "lyrics", Language: "en"}, domain.NewID())
	matchingError(t, err, "CONFLICT")
	done, err := f.s.CheckWriteback(ctx, "admin", j.ID, f.target, 2)
	if err != nil || done {
		t.Fatal("recovered task lost old execution binding", err)
	}
	r, o := f.result(j, true)
	if _, err = f.s.CommitWriteback(ctx, "admin", j.ID, r, o); err != nil {
		t.Fatal("recovered commit failed", err)
	}
	selection, err := f.s.ContentSelection(ctx, "admin", f.track.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.UnlockContent(ctx, "admin", mediastore.UnlockRequest{SourceTrackID: f.track.ID, ExpectedRevision: selection.Revision, Field: "lyrics", Language: "en"}, domain.NewID()); err != nil {
		t.Fatal("completed recovery kept selection fence", err)
	}
}
