package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

const garbageUnreferenced = ` NOT EXISTS(SELECT 1 FROM media_content_objects c WHERE c.id=p.object_id)
 AND NOT EXISTS(SELECT 1 FROM media_derived_objects d WHERE d.id=p.object_id)
 AND NOT EXISTS(SELECT 1 FROM fingerprint_indexes i WHERE i.object_id=p.object_id)
 AND NOT EXISTS(SELECT 1 FROM media_publications active WHERE active.object_id=p.object_id AND active.status IN ('prepared','written','published','referenced'))
 AND NOT EXISTS(SELECT 1 FROM media_backup_objects b WHERE b.object_id=p.object_id)`

func readGarbage(row rowScanner) (mediastore.Garbage, error) {
	var item mediastore.Garbage
	var created, updated string
	err := row.Scan(&item.ID, &item.PublicationID, &item.ObjectID, &item.State, &created, &updated)
	if err != nil {
		return item, err
	}
	if !mediaID(item.ID) || !mediaID(item.PublicationID) || !mediaID(item.ObjectID) {
		return item, domain.Fail("CONFLICT", "Invalid media garbage identity")
	}
	if item.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return item, err
	}
	item.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return item, err
}

func (s *Store) ClaimMediaGarbage(ctx context.Context, olderThan time.Time, limit int) ([]mediastore.Garbage, error) {
	if olderThan.IsZero() || olderThan.After(time.Now()) || limit < 1 || limit > 128 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid media collection bounds")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Previously claimed work is returned before new work and can be repeated
	// after a crash between deleting files and committing the deletion receipt.
	rows, err := tx.QueryContext(ctx, `SELECT p.id,p.object_id,p.updated_at FROM media_publications p LEFT JOIN media_garbage g ON g.publication_id=p.id
	 WHERE p.status='aborted' AND julianday(p.updated_at)<=julianday(?) AND (g.id IS NULL OR g.state='claimed') AND `+garbageUnreferenced+`
 ORDER BY CASE WHEN g.state='claimed' THEN 0 ELSE 1 END,p.id LIMIT ?`, olderThan.UTC().Format(time.RFC3339Nano), limit)
	if err != nil {
		return nil, err
	}
	type candidate struct{ id, object string }
	var all []candidate
	for rows.Next() {
		var c candidate
		var updated string
		if err = rows.Scan(&c.id, &c.object, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		at, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			rows.Close()
			return nil, err
		}
		if at.After(olderThan) {
			continue
		}
		if !mediaID(c.id) || !mediaID(c.object) {
			rows.Close()
			return nil, domain.Fail("CONFLICT", "Invalid media garbage identity")
		}
		all = append(all, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []mediastore.Garbage{}
	for _, c := range all {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err = tx.ExecContext(ctx, "INSERT INTO media_garbage(id,publication_id,object_id,state,created_at,updated_at) VALUES(?,?,?,'claimed',?,?) ON CONFLICT(publication_id) DO NOTHING", domain.NewID(), c.id, c.object, now, now); err != nil {
			return nil, err
		}
		item, err := readGarbage(tx.QueryRowContext(ctx, "SELECT id,publication_id,object_id,state,created_at,updated_at FROM media_garbage WHERE publication_id=?", c.id))
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, tx.Commit()
}

func (s *Store) DeleteMediaGarbage(ctx context.Context, id string, remove func(mediastore.Garbage) error) error {
	if !mediaID(id) || remove == nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid media deletion")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item, err := readGarbage(tx.QueryRowContext(ctx, "SELECT id,publication_id,object_id,state,created_at,updated_at FROM media_garbage WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Media deletion not found")
	}
	if err != nil {
		return err
	}
	if item.State == "deleted" {
		return nil
	}
	var allowed int
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_publications p WHERE p.id=? AND p.object_id=? AND p.status='aborted' AND "+garbageUnreferenced, item.PublicationID, item.ObjectID).Scan(&allowed)
	if err != nil {
		return err
	}
	if allowed != 1 {
		return domain.Fail("CONFLICT", "Media file gained a protected reference")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = remove(item); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE media_garbage SET state='deleted',updated_at=? WHERE id=? AND state='claimed'", time.Now().UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return err
	}
	return tx.Commit()
}

var _ mediastore.GarbageRepository = (*Store)(nil)
