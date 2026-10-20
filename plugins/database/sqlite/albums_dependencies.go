package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

const maxAlbumReleaseReferences = 1000

func albumDependentReleases(ctx context.Context, tx *sql.Tx, owner, id string) ([]string, error) {
	return readAlbumIDs(ctx, tx, "SELECT r.id FROM releases r JOIN release_albums m ON m.release_id=r.id WHERE r.owner=? AND m.album_id=? ORDER BY r.id", owner, id)
}
func advanceAlbumDependents(ctx context.Context, tx *sql.Tx, owner string, ids []string) error {
	for _, id := range ids {
		r, err := readRelease(ctx, tx, owner, id)
		if err != nil {
			return err
		}
		r.Revision++
		_, err = tx.ExecContext(ctx, "UPDATE releases SET revision=?,updated_at=? WHERE owner=? AND id=?", r.Revision, time.Now().UTC().Format(time.RFC3339Nano), owner, id)
		if err != nil {
			return err
		}
		if err = recordAlbumChange(ctx, tx, owner, albums.Change{Target: albums.Target{Kind: "release", ID: id}, Area: "members", Revision: r.Revision, Operation: "memberAlbumUpdated", PreviousMembers: r.SourceAlbumIDs, Members: r.SourceAlbumIDs}); err != nil {
			return err
		}
	}
	return nil
}
func checkReleaseFanout(ctx context.Context, tx *sql.Tx, album, release string) error {
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM release_albums WHERE album_id=? AND release_id!=?", album, release).Scan(&count); err != nil {
		return err
	}
	if count >= maxAlbumReleaseReferences {
		return domain.Fail("RATE_LIMITED", "Source album release-reference limit exceeded")
	}
	return nil
}
