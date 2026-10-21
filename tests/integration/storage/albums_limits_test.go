package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
)

func TestAlbumReferenceLimitFailsBeforeChangingAnyMember(t *testing.T) {
	s, _, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	a := albumCreate(t, s, "admin", "local", "Bounded", tracks[0].ID)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for range 1000 {
		id := domain.NewID()
		if _, err = tx.ExecContext(ctx, "INSERT INTO releases VALUES(?,?,?,1,?,?)", id, "admin", "fixture", now, now); err != nil {
			t.Fatal(err)
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO release_albums VALUES(?,?,0)", id, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateRelease(ctx, "admin", albums.CreateReleaseRequest{Name: "overflow", SourceAlbumIDs: []string{a.ID}}, domain.NewID())
	matchingError(t, err, "RATE_LIMITED")
	var count int
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM releases").Scan(&count); err != nil || count != 1000 {
		t.Fatal("overflow left partial release", count, err)
	}
	// A malformed oversized relationship set also cannot produce a partial
	// propagation, even if it was introduced outside the normal write interface.
	id := domain.NewID()
	if _, err = s.DB.Exec("INSERT INTO releases VALUES(?,?,?,1,?,?)", id, "admin", "external fixture", now, now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB.Exec("INSERT INTO release_albums VALUES(?,?,0)", id, a.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.UpdateSourceAlbum(ctx, "admin", a.ID, albums.UpdateSourceAlbumRequest{ExpectedRevision: 1, Name: "must not publish", SourceTrackIDs: []string{}}, domain.NewID())
	matchingError(t, err, "RATE_LIMITED")
	current, err := s.SourceAlbum(ctx, "admin", a.ID)
	if err != nil || current.Revision != 1 || current.Name != a.Name || len(current.SourceTrackIDs) != 1 {
		t.Fatal("oversized propagation changed source album", err)
	}
	if err = s.DB.QueryRow("SELECT COUNT(*) FROM releases WHERE revision!=1").Scan(&count); err != nil || count != 0 {
		t.Fatal("some releases advanced before failure", err)
	}
}
