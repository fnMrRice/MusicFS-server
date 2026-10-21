package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

func personalTable(kind string) (string, string) {
	if kind == personal.Favorites {
		return "personal_favorites", "created_at"
	}
	return "personal_recently_played", "played_at"
}

func bumpPersonalRevision(ctx context.Context, tx *sql.Tx, owner, kind string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO personal_collection_revisions VALUES(?,?,1) ON CONFLICT(owner,kind) DO UPDATE SET revision=revision+1", owner, kind)
	return err
}

func readPersonalTrack(ctx context.Context, tx *sql.Tx, owner, id string) (library.Track, error) {
	var track library.Track
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM tracks WHERE owner=? AND id=?", owner, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return track, domain.Fail("NOT_FOUND", "Track was not found")
	}
	if err != nil {
		return track, err
	}
	err = json.Unmarshal(raw, &track)
	return track, err
}

func (s *Store) PersonalCollection(ctx context.Context, owner, kind, cursor string, limit int) (personal.Page, error) {
	page := personal.Page{Items: []personal.Entry{}}
	if !personal.ValidKind(kind) || limit < 1 || limit > 100 {
		return page, personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if kind == personal.Recent {
		if err = pruneRecent(ctx, tx, owner); err != nil {
			return page, err
		}
	}
	var collectionRevision, libraryRevision int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT revision FROM personal_collection_revisions WHERE owner=? AND kind=?),0),(SELECT revision FROM library_revision)", owner, kind).Scan(&collectionRevision, &libraryRevision); err != nil {
		return page, err
	}
	page.Revision = fmt.Sprintf("%d/%d", collectionRevision, libraryRevision)
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, "DELETE FROM personal_collection_cursors WHERE julianday(expires_at)<=julianday(?)", now.Format(time.RFC3339Nano)); err != nil {
		return page, err
	}
	position := 0
	if cursor != "" {
		err = tx.QueryRowContext(ctx, "SELECT position FROM personal_collection_cursors WHERE id=? AND owner=? AND kind=? AND revision=?", cursor, owner, kind, page.Revision).Scan(&position)
		if errors.Is(err, sql.ErrNoRows) {
			return page, domain.Fail("CURSOR_EXPIRED", "Collection changed or cursor expired; refresh the list")
		}
		if err != nil {
			return page, err
		}
	}
	table, column := personalTable(kind)
	rows, err := tx.QueryContext(ctx, "SELECT t.document,c."+column+" FROM "+table+" c JOIN tracks t ON t.owner=c.owner AND t.id=c.source_track_id WHERE c.owner=? ORDER BY c."+column+" DESC,c.source_track_id LIMIT ? OFFSET ?", owner, limit+1, position)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var raw []byte
		var stamp string
		var entry personal.Entry
		if err = rows.Scan(&raw, &stamp); err == nil {
			err = json.Unmarshal(raw, &entry.Track)
		}
		if err == nil {
			entry.RecordedAt, err = time.Parse(time.RFC3339Nano, stamp)
		}
		if err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		var next string
		err = tx.QueryRowContext(ctx, "SELECT id FROM personal_collection_cursors WHERE owner=? AND kind=? AND revision=? AND position=? LIMIT 1", owner, kind, page.Revision, position+limit).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM personal_collection_cursors WHERE owner=?", owner).Scan(&count); err != nil {
				return page, err
			}
			if count >= 2048 {
				return page, domain.Fail("RATE_LIMITED", "Too many active personal collection cursors")
			}
			next = domain.NewID()
			_, err = tx.ExecContext(ctx, "INSERT INTO personal_collection_cursors VALUES(?,?,?,?,?,?)", next, owner, kind, page.Revision, position+limit, now.Add(10*time.Minute).Format(time.RFC3339Nano))
		}
		if err != nil {
			return page, err
		}
		page.NextCursor = &next
	}
	return page, tx.Commit()
}

func (s *Store) PutFavorite(ctx context.Context, owner, id string) (personal.Entry, error) {
	var entry personal.Entry
	if !personal.ValidID(id) {
		return entry, personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return entry, err
	}
	defer tx.Rollback()
	entry.Track, err = readPersonalTrack(ctx, tx, owner, id)
	if err != nil {
		return entry, err
	}
	var stamp string
	err = tx.QueryRowContext(ctx, "SELECT created_at FROM personal_favorites WHERE owner=? AND source_track_id=?", owner, id).Scan(&stamp)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM personal_favorites WHERE owner=?", owner).Scan(&count); err != nil {
			return entry, err
		}
		if count >= personal.MaxFavorites {
			return entry, domain.Fail("RATE_LIMITED", "Favorite collection limit reached")
		}
		stamp = time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
		if _, err = tx.ExecContext(ctx, "INSERT INTO personal_favorites VALUES(?,?,?)", owner, id, stamp); err != nil {
			return entry, err
		}
		err = bumpPersonalRevision(ctx, tx, owner, personal.Favorites)
	}
	if err != nil {
		return entry, err
	}
	entry.RecordedAt, err = time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return entry, err
	}
	return entry, tx.Commit()
}

func (s *Store) DeletePersonalCollection(ctx context.Context, owner, kind, id string) error {
	if !personal.ValidKind(kind) || id != "" && !personal.ValidID(id) {
		return personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	table, _ := personalTable(kind)
	result, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE owner=? AND (?='' OR source_track_id=?)", owner, id, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		if err = bumpPersonalRevision(ctx, tx, owner, kind); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM personal_collection_cursors WHERE owner=? AND kind=?", owner, kind); err != nil {
		return err
	}
	if kind == personal.Recent {
		if _, err = tx.ExecContext(ctx, "DELETE FROM personal_playback_receipts WHERE owner=? AND (?='' OR source_track_id=?)", owner, id, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
