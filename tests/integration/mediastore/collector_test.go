package mediastore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func abandoned(t *testing.T, f fixture) mediastore.Publication {
	t.Helper()
	h := sha256.Sum256([]byte("orphan"))
	meta := f.metadata
	meta.Format = "text"
	p, _, err := f.store.PrepareContent(context.Background(), "admin", mediastore.UploadInput{Metadata: meta, MIME: "text/plain; charset=utf-8", Size: 6, SHA256: hex.EncodeToString(h[:])}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.store.SetPublicationStage(context.Background(), "admin", p.ID, "prepared", "aborted"); err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.DB.Exec("UPDATE media_publications SET updated_at=? WHERE id=?", time.Now().Add(-48*time.Hour).UTC().Format(time.RFC3339Nano), p.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCollectorDeletesOnlyClaimedAbandonedPathsAndContinuesPastDamage(t *testing.T) {
	f := setup(t)
	protected := upload(t, f, "unselected manual preview")
	bad := abandoned(t, f)
	good := abandoned(t, f)
	if err := os.Mkdir(filepath.Join(f.root, "objects", bad.Object.ID), 0700); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"staging", "objects"} {
		if err := os.WriteFile(filepath.Join(f.root, folder, good.Object.ID), []byte("orphan"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	unknown := filepath.Join(f.root, "objects", domain.NewID())
	if err := os.WriteFile(unknown, []byte("unknown ownership"), 0600); err != nil {
		t.Fatal(err)
	}
	collector, err := mediastore.NewCollector(f.store, f.files, 0)
	if err != nil {
		t.Fatal(err)
	}
	count, err := collector.Collect(context.Background(), time.Now())
	if err == nil || count != 1 {
		t.Fatalf("damaged entry blocked valid garbage: %d %v", count, err)
	}
	for _, folder := range []string{"staging", "objects"} {
		if _, err = os.Stat(filepath.Join(f.root, folder, good.Object.ID)); !os.IsNotExist(err) {
			t.Fatal("abandoned path survived")
		}
	}
	for _, path := range []string{unknown, filepath.Join(f.root, "objects", protected.ID), filepath.Join(f.root, "objects", bad.Object.ID)} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal("protected or unsupported path removed", err)
		}
	}
	// Repair only the synthetic non-file obstruction, then retry its durable claim.
	if err = os.Remove(filepath.Join(f.root, "objects", bad.Object.ID)); err != nil {
		t.Fatal(err)
	}
	if count, err = collector.Collect(context.Background(), time.Now()); err != nil || count != 1 {
		t.Fatal("claimed deletion did not resume", count, err)
	}
	if count, err = collector.Collect(context.Background(), time.Now()); err != nil || count != 0 {
		t.Fatal("completed garbage retried", count, err)
	}
	if _, err = mediastore.NewCollector(f.store, f.files, time.Minute); err == nil {
		t.Fatal("unsafe grace accepted")
	}
}
