package storage_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func abandonedMedia(t *testing.T, s *sqlite.Store, track library.Track, at time.Time) mediastore.Publication {
	t.Helper()
	ctx := context.Background()
	p, _, err := s.PrepareContent(ctx, "admin", contentInput(track, "en", "abandoned content"), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetPublicationStage(ctx, "admin", p.ID, "prepared", "aborted"); err != nil {
		t.Fatal(err)
	}
	p.Status, p.UpdatedAt = "aborted", at.UTC()
	raw, _ := json.Marshal(p)
	if _, err = s.DB.Exec("UPDATE media_publications SET document=?,updated_at=? WHERE id=?", raw, at.UTC().Format(time.RFC3339Nano), p.ID); err != nil {
		t.Fatal(err)
	}
	return p
}

func pinGarbage(t *testing.T, s *sqlite.Store, p mediastore.Publication) {
	t.Helper()
	id := domain.NewID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.DB.Exec("INSERT INTO media_backups(id,database_path,state,created_at,updated_at) VALUES(?,?,'prepared',?,?)", id, filepath.Join(t.TempDir(), "pin.sqlite"), now, now); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(mediastore.BackupObject{Owner: p.Owner, PublicationID: p.ID, Object: p.Object, State: "missing"})
	if _, err := s.DB.Exec("INSERT INTO media_backup_objects VALUES(?,?,?,?,'missing',?)", id, p.Object.ID, p.Owner, p.ID, raw); err != nil {
		t.Fatal(err)
	}
}

func TestMediaGarbageProtectsAllReferencesPinsAndGrace(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	cutoff := time.Now().Add(-24 * time.Hour)
	claimable := abandonedMedia(t, s, tracks[0], old)
	abandonedMedia(t, s, tracks[0], time.Now())
	pinned := abandonedMedia(t, s, tracks[0], old)
	pinGarbage(t, s, pinned)
	preview := contentPublish(t, s, "admin", contentInput(tracks[0], "en", "unselected manual preview"))
	index := matchingFingerprint(t, s, fpTarget(tracks[0]))
	if err := s.InvalidateFingerprint(ctx, "admin", index.ID); err != nil {
		t.Fatal(err)
	}
	// Defensive invariant: even an inconsistent aborted journal cannot authorize
	// removal of a content preview or a historical fingerprint reference.
	if _, err := s.DB.Exec("UPDATE media_publications SET status='aborted',updated_at=? WHERE object_id IN (?,?)", old.UTC().Format(time.RFC3339Nano), preview.ID, index.Object.ID); err != nil {
		t.Fatal(err)
	}
	items, err := s.ClaimMediaGarbage(ctx, cutoff, 128)
	if err != nil || len(items) != 1 || items[0].ObjectID != claimable.Object.ID {
		t.Fatalf("protected media entered deletion: %+v %v", items, err)
	}
	// Pins may arrive between a claim and deletion. The callback must not run.
	pinGarbage(t, s, claimable)
	called := false
	err = s.DeleteMediaGarbage(ctx, items[0].ID, func(mediastore.Garbage) error { called = true; return nil })
	matchingError(t, err, "CONFLICT")
	if called {
		t.Fatal("reference recheck happened after deletion")
	}
	for _, limit := range []int{0, 129} {
		if _, err = s.ClaimMediaGarbage(ctx, cutoff, limit); err == nil {
			t.Fatal("unbounded garbage batch accepted")
		}
	}
	if _, err = s.ClaimMediaGarbage(ctx, time.Now().Add(time.Hour), 1); err == nil {
		t.Fatal("future collection cutoff accepted")
	}
}

func TestMediaGarbageDeletionIsRepeatableAfterInterruptedCommit(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	cutoff := time.Now().Add(-time.Hour)
	p := abandonedMedia(t, s, tracks[0], cutoff.Add(-time.Hour))
	items, err := s.ClaimMediaGarbage(ctx, cutoff, 1)
	if err != nil || len(items) != 1 {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), p.Object.ID)
	if err = os.WriteFile(path, []byte("partial or orphan bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	err = s.DeleteMediaGarbage(ctx, items[0].ID, func(g mediastore.Garbage) error {
		if g.ObjectID != p.Object.ID {
			t.Fatal("unexpected path identity")
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		return errors.New("simulated interruption after file deletion")
	})
	if err == nil {
		t.Fatal("interrupted deletion committed")
	}
	var sequence int
	var name, dbPath string
	if err = s.DB.QueryRow("PRAGMA database_list").Scan(&sequence, &name, &dbPath); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	retry, err := reopened.ClaimMediaGarbage(ctx, cutoff, 1)
	if err != nil || len(retry) != 1 || retry[0] != items[0] {
		t.Fatal("restart lost claimed deletion", err)
	}
	var callbacks atomic.Int32
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = reopened.DeleteMediaGarbage(ctx, retry[0].ID, func(g mediastore.Garbage) error {
				callbacks.Add(1)
				err := os.Remove(path)
				if os.IsNotExist(err) {
					return nil
				}
				return err
			})
		}(i)
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || callbacks.Load() != 1 {
		t.Fatal("repeated collectors repeated committed deletion", errs, callbacks.Load())
	}
	more, err := reopened.ClaimMediaGarbage(ctx, cutoff, 128)
	if err != nil || len(more) != 0 {
		t.Fatal("deleted task became claimable", err)
	}
}

func TestMediaGarbageTimestampBoundaryIsExact(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	cutoff := time.Now().Add(-time.Hour).Truncate(time.Second)
	abandonedMedia(t, s, tracks[0], cutoff.Add(time.Nanosecond))
	eligible := abandonedMedia(t, s, tracks[0], cutoff)
	items, err := s.ClaimMediaGarbage(ctx, cutoff, 128)
	if err != nil || len(items) != 1 || items[0].ObjectID != eligible.Object.ID {
		t.Fatal("variable timestamp precision bypassed grace period", items, err)
	}
}
