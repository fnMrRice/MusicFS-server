package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
)

func (s *Store) CreateCatalogQuery(ctx context.Context, owner string, input catalog.Query, limit int, key string) (queryResult catalog.Page, queryErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", "")
	finish := pluginlog.Operation(ctx, "catalog.query.create")
	defer func() { finish(queryErr) }()
	q, err := input.Normalize()
	if err != nil {
		return catalog.Page{}, err
	}
	limit, err = catalog.PageLimit(limit)
	if err != nil {
		return catalog.Page{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return catalog.Page{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, "DELETE FROM catalog_query_items WHERE query_id IN (SELECT id FROM catalog_queries WHERE julianday(expires_at)<=julianday(?) OR cancelled=1)", now.Format(time.RFC3339Nano)); err != nil {
		return catalog.Page{}, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM catalog_queries WHERE julianday(expires_at)<julianday(?)", now.Add(-24*time.Hour).Format(time.RFC3339Nano)); err != nil {
		return catalog.Page{}, err
	}
	raw, _ := json.Marshal(q)
	if key != "" {
		var id string
		var previous []byte
		err = tx.QueryRowContext(ctx, "SELECT id,query FROM catalog_queries WHERE owner=? AND idempotency_key=?", owner, key).Scan(&id, &previous)
		if err == nil {
			if !bytes.Equal(raw, previous) {
				return catalog.Page{}, domain.Fail("CONFLICT", "Idempotency key was used for another catalog query")
			}
			page, err := readCatalogPage(ctx, tx, owner, id, "", limit, nil)
			if err != nil {
				return page, err
			}
			return page, tx.Commit()
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return catalog.Page{}, err
		}
	}
	var count, existingBytes int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_queries WHERE owner=? AND cancelled=0 AND julianday(expires_at)>julianday(?)", owner, now.Format(time.RFC3339Nano)).Scan(&count); err != nil {
		return catalog.Page{}, err
	}
	if count >= 64 {
		return catalog.Page{}, domain.Fail("RATE_LIMITED", "Too many active catalog queries")
	}
	data, err := readCatalogData(ctx, tx, owner, q.ProviderInstanceID)
	if err != nil {
		return catalog.Page{}, err
	}
	items, err := catalogResults(owner, data, q)
	if err != nil {
		return catalog.Page{}, err
	}
	size := 0
	for _, item := range items {
		size += len(item)
	}
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE(SUM(length(i.document)),0) FROM catalog_query_items i JOIN catalog_queries q ON q.id=i.query_id WHERE q.owner=?", owner).Scan(&existingBytes); err != nil {
		return catalog.Page{}, err
	}
	if size > catalog.MaxSnapshotBytes || existingBytes+size > 2*catalog.MaxSnapshotBytes {
		return catalog.Page{}, domain.Fail("RATE_LIMITED", "Catalog snapshot byte budget exceeded")
	}
	id := domain.NewID()
	_, err = tx.ExecContext(ctx, "INSERT INTO catalog_queries(id,owner,query,revision,total,expires_at,idempotency_key) VALUES(?,?,?,?,?,?,?)", id, owner, raw, data.revision, len(items), now.Add(catalog.SnapshotTTL).Format(time.RFC3339Nano), key)
	if err != nil {
		return catalog.Page{}, err
	}
	for i, item := range items {
		if _, err = tx.ExecContext(ctx, "INSERT INTO catalog_query_items(query_id,position,document) VALUES(?,?,?)", id, i, []byte(item)); err != nil {
			return catalog.Page{}, err
		}
	}
	page, err := readCatalogPage(ctx, tx, owner, id, "", limit, nil)
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}

func (s *Store) CatalogQueryPage(ctx context.Context, owner, id, cursor string, limit int, expected *catalog.Query) (queryResult catalog.Page, queryErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", "")
	finish := pluginlog.Operation(ctx, "catalog.query.page")
	defer func() { finish(queryErr) }()
	limit, err := catalog.PageLimit(limit)
	if err != nil {
		return catalog.Page{}, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return catalog.Page{}, err
	}
	defer tx.Rollback()
	page, err := readCatalogPage(ctx, tx, owner, id, cursor, limit, expected)
	if err != nil {
		return page, err
	}
	return page, tx.Commit()
}
func readCatalogPage(ctx context.Context, tx *sql.Tx, owner, id, cursor string, limit int, expected *catalog.Query) (catalog.Page, error) {
	page := catalog.Page{Items: []json.RawMessage{}}
	position := 0
	if cursor != "" {
		var target string
		err := tx.QueryRowContext(ctx, "SELECT c.query_id,c.position FROM catalog_query_cursors c JOIN catalog_queries q ON q.id=c.query_id WHERE c.id=? AND q.owner=?", cursor, owner).Scan(&target, &position)
		if errors.Is(err, sql.ErrNoRows) || err == nil && id != "" && target != id {
			return page, expiredCatalog()
		}
		if err != nil {
			return page, err
		}
		id = target
	}
	var raw []byte
	var expires string
	var cancelled bool
	err := tx.QueryRowContext(ctx, "SELECT query,revision,total,expires_at,cancelled FROM catalog_queries WHERE owner=? AND id=?", owner, id).Scan(&raw, &page.CatalogRevision, &page.Total, &expires, &cancelled)
	if errors.Is(err, sql.ErrNoRows) {
		return page, domain.Fail("NOT_FOUND", "Catalog query not found")
	}
	if err != nil {
		return page, err
	}
	if err = json.Unmarshal(raw, &page.Query); err != nil {
		return page, err
	}
	if page.ExpiresAt, err = time.Parse(time.RFC3339Nano, expires); err != nil {
		return page, err
	}
	if cancelled || !page.ExpiresAt.After(time.Now().UTC()) {
		return page, expiredCatalog()
	}
	if expected != nil {
		given, _ := json.Marshal(expected)
		if !bytes.Equal(given, raw) {
			return page, expiredCatalog()
		}
	}
	page.QueryID = id
	rows, err := tx.QueryContext(ctx, "SELECT document FROM catalog_query_items WHERE query_id=? AND position>=? ORDER BY position LIMIT ?", id, position, limit)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var item []byte
		if err = rows.Scan(&item); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, json.RawMessage(item))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if position+len(page.Items) < page.Total {
		var next string
		err = tx.QueryRowContext(ctx, "SELECT id FROM catalog_query_cursors WHERE query_id=? AND position=?", id, position+len(page.Items)).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			next = domain.NewID()
			_, err = tx.ExecContext(ctx, "INSERT INTO catalog_query_cursors(id,query_id,position) VALUES(?,?,?)", next, id, position+len(page.Items))
		}
		if err != nil {
			return page, err
		}
		page.NextCursor = &next
	}
	return page, nil
}
func expiredCatalog() error {
	return domain.Fail("CURSOR_EXPIRED", "Catalog query expired, was cancelled, or does not match this request")
}
func (s *Store) CancelCatalogQuery(ctx context.Context, owner, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "UPDATE catalog_queries SET cancelled=1 WHERE owner=? AND id=?", owner, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.Fail("NOT_FOUND", "Catalog query not found")
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM catalog_query_items WHERE query_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}
