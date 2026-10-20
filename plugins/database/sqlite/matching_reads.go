package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/matching"
)

func readRecording(ctx context.Context, tx *sql.Tx, owner, id string) (matching.Recording, error) {
	r := matching.Recording{SourceTrackIDs: []string{}}
	var created, updated string
	err := tx.QueryRowContext(ctx, "SELECT id,created_at,updated_at FROM recordings WHERE owner=? AND id=?", owner, id).Scan(&r.ID, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return r, domain.Fail("NOT_FOUND", "Recording not found")
	}
	if err != nil {
		return r, err
	}
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return r, err
	}
	if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return r, err
	}
	r.Revision, err = matchingRevision(ctx, tx, owner)
	if err != nil {
		return r, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT l.source_id FROM recording_source_links l JOIN tracks t ON t.id=l.source_id AND t.owner=l.owner WHERE l.owner=? AND l.recording_id=? ORDER BY l.source_id", owner, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		if err = rows.Scan(&source); err != nil {
			return r, err
		}
		r.SourceTrackIDs = append(r.SourceTrackIDs, source)
	}
	return r, rows.Err()
}

func (s *Store) Recording(ctx context.Context, owner, id string) (matching.Recording, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return matching.Recording{}, err
	}
	defer tx.Rollback()
	return readRecording(ctx, tx, owner, id)
}

func (s *Store) GetLink(ctx context.Context, owner, source string) (matching.Link, error) {
	l := matching.Link{SourceTrackID: source, Decision: "unmatched"}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return l, err
	}
	defer tx.Rollback()
	if err = visibleSources(ctx, tx, owner, []string{source}); err != nil {
		return l, err
	}
	var id sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT recording_id,decision FROM recording_source_links WHERE owner=? AND source_id=?", owner, source).Scan(&id, &l.Decision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return l, err
	}
	l.RecordingID = strptr(id.String)
	l.Revision, err = matchingRevision(ctx, tx, owner)
	return l, err
}

func (s *Store) ChangesAfter(ctx context.Context, owner string, after int64, limit int) (matching.ChangePage, error) {
	page := matching.ChangePage{Items: []matching.LinkChange{}}
	if after < 0 || limit < 1 || limit > 200 {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid matching audit page")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	page.Revision, err = matchingRevision(ctx, tx, owner)
	if err != nil {
		return page, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM link_changes WHERE owner=? AND sequence>? ORDER BY sequence LIMIT ?", owner, after, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var change matching.LinkChange
		if err = rows.Scan(&raw); err != nil {
			return page, err
		}
		if err = json.Unmarshal(raw, &change); err != nil {
			return page, err
		}
		change.Owner = owner
		page.Items = append(page.Items, change)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		next := page.Items[limit-1].Sequence
		page.NextAfter = &next
	}
	return page, nil
}
