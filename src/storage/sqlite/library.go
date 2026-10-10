package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Store) Publish(ctx context.Context, supplied jobs.Job, publication library.ScanPublication) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := activeScan(ctx, tx, supplied)
	if err != nil {
		return err
	}
	scopes, err := validatePublication(j, publication)
	if err != nil {
		return err
	}
	observations := publication.Observations
	rows, err := tx.QueryContext(ctx, "SELECT document,root_id,path FROM tracks WHERE owner=? AND provider=?", j.Owner, j.ProviderInstanceID)
	if err != nil {
		return err
	}
	old := map[string]library.Track{}
	for rows.Next() {
		var raw []byte
		var root, path string
		if err := rows.Scan(&raw, &root, &path); err != nil {
			rows.Close()
			return err
		}
		var t library.Track
		if err := json.Unmarshal(raw, &t); err != nil {
			rows.Close()
			return err
		}
		t.RootID, t.Path = root, path
		old[t.ID] = t
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	seenExternal := map[string]bool{}
	for _, o := range observations {
		if o.ExternalID == "" || o.ObservedVersion == "" || seenExternal[o.ExternalID] {
			return domain.Fail("CONFLICT", "Scan contains an invalid or repeated track identity")
		}
		seenExternal[o.ExternalID] = true
		var id, asset string
		err := tx.QueryRowContext(ctx, "SELECT id,asset_id FROM tracks WHERE owner=? AND provider=? AND external_id=?", j.Owner, j.ProviderInstanceID, o.ExternalID).Scan(&id, &asset)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if id == "" {
			id = domain.NewID()
		}
		current, err := scanAsset(ctx, tx, j.Owner, id, asset, o)
		if err != nil {
			return err
		}
		asset = current.ID
		artists := o.Artists
		if artists == nil {
			artists = []string{}
		}
		t := library.Track{ID: id, ProviderID: j.ProviderInstanceID, DisplayName: o.DisplayName, Title: o.Title, Artists: artists, DurationMs: o.DurationMs, Codec: o.Codec, Container: o.Container, BitrateBps: current.BitrateBps, Availability: "available", AssetID: asset, Asset: &current}
		raw, _ := json.Marshal(t)
		_, err = tx.ExecContext(ctx, "INSERT INTO tracks(id,owner,provider,external_id,root_id,path,version,asset_id,mime,size,document) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET root_id=excluded.root_id,path=excluded.path,version=excluded.version,asset_id=excluded.asset_id,mime=excluded.mime,size=excluded.size,document=excluded.document", id, j.Owner, j.ProviderInstanceID, o.ExternalID, o.RootID, o.RelativePath, o.ObservedVersion, asset, o.MIME, o.SizeBytes, raw)
		if err != nil {
			return err
		}
		seen[id] = true
		if err = enqueueScanAutomation(ctx, tx, j, o.RootID, id, asset, o.ObservedVersion); err != nil {
			return err
		}
	}
	for id, t := range old {
		if !seen[id] && scopes[t.RootID].Contains(t.RootID, t.Path) {
			t.Availability = "missing"
			if err := invalidateAsset(ctx, tx, j.Owner, t.AssetID); err != nil {
				return err
			}
			a, err := readAsset(ctx, tx, j.Owner, t.AssetID)
			if err != nil {
				return err
			}
			t.Asset, t.BitrateBps = &a, a.BitrateBps
			raw, _ := json.Marshal(t)
			if _, err = tx.ExecContext(ctx, "UPDATE tracks SET document=? WHERE id=?", raw, id); err != nil {
				return err
			}
		}
	}
	if _, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) FindTrack(ctx context.Context, user, id string) (library.Track, error) {
	var t library.Track
	var raw []byte
	var external, root, path, version, mime, owner string
	var size int64
	err := s.DB.QueryRowContext(ctx, "SELECT document,owner,external_id,root_id,path,version,mime,size FROM tracks WHERE id=? AND owner=?", id, user).Scan(&raw, &owner, &external, &root, &path, &version, &mime, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return t, domain.Fail("NOT_FOUND", "Track not found")
	}
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal(raw, &t); err != nil {
		return t, err
	}
	t.Owner = owner
	t.ExternalID = external
	t.RootID = root
	t.Path = path
	t.Version = version
	t.MIME = mime
	t.Size = size
	return t, nil
}
func (s *Store) ProviderScanState(ctx context.Context, user, id string) string {
	var state string
	if s.DB.QueryRowContext(ctx, "SELECT state FROM jobs WHERE owner=? AND target=? ORDER BY CASE state WHEN 'running' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,created_at DESC,id DESC LIMIT 1", user, id).Scan(&state) != nil {
		return "idle"
	}
	return state
}

func (s *Store) ListTracks(ctx context.Context, user, provider, cursor string, limit int) (library.Page, error) {
	page := library.Page{Items: []library.Track{}}
	if limit < 1 || limit > 200 {
		return page, domain.Fail("INVALID_ARGUMENT", "limit must be between 1 and 200")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "DELETE FROM library_snapshots WHERE julianday(expires_at)<julianday(?)", now); err != nil {
		return page, err
	}
	var snapshot string
	var position int
	if cursor != "" {
		err = tx.QueryRowContext(ctx, "SELECT s.id,s.revision,c.position FROM library_cursors c JOIN library_snapshots s ON s.id=c.snapshot_id WHERE c.id=? AND s.owner=? AND s.provider=?", cursor, user, provider).Scan(&snapshot, &page.LibraryRevision, &position)
		if errors.Is(err, sql.ErrNoRows) {
			return page, domain.Fail("CURSOR_EXPIRED", "Cursor is expired or does not match this request")
		}
		if err != nil {
			return page, err
		}
	} else {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM library_snapshots WHERE owner=?", user).Scan(&count); err != nil {
			return page, err
		}
		if count >= 64 {
			return page, domain.Fail("RATE_LIMITED", "Too many active list snapshots")
		}
		snapshot = domain.NewID()
		var revision int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&revision); err != nil {
			return page, err
		}
		page.LibraryRevision = fmt.Sprint(revision)
		_, err = tx.ExecContext(ctx, "INSERT INTO library_snapshots VALUES(?,?,?,?,?)", snapshot, user, provider, page.LibraryRevision, time.Now().UTC().Add(10*time.Minute).Format(time.RFC3339Nano))
		if err != nil {
			return page, err
		}
		rows, err := tx.QueryContext(ctx, "SELECT document FROM tracks WHERE owner=? AND (?='' OR provider=?)", user, provider, provider)
		if err != nil {
			return page, err
		}
		var items []library.Track
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return page, err
			}
			var t library.Track
			if err = json.Unmarshal(raw, &t); err != nil {
				rows.Close()
				return page, err
			}
			items = append(items, t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return page, err
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].DisplayName == items[j].DisplayName {
				return items[i].ID < items[j].ID
			}
			return items[i].DisplayName < items[j].DisplayName
		})
		for i, t := range items {
			raw, _ := json.Marshal(t)
			if _, err = tx.ExecContext(ctx, "INSERT INTO library_snapshot_items VALUES(?,?,?)", snapshot, i, raw); err != nil {
				return page, err
			}
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM library_snapshot_items WHERE snapshot_id=? AND position>=? ORDER BY position LIMIT ?", snapshot, position, limit+1)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return page, err
		}
		var t library.Track
		if err = json.Unmarshal(raw, &t); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		var next string
		err = tx.QueryRowContext(ctx, "SELECT id FROM library_cursors WHERE snapshot_id=? AND position=? LIMIT 1", snapshot, position+limit).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			next = domain.NewID()
			_, err = tx.ExecContext(ctx, "INSERT INTO library_cursors VALUES(?,?,?)", next, snapshot, position+limit)
		}
		if err != nil {
			return page, err
		}
		page.NextCursor = &next
	} else if cursor == "" {
		// Single-page refreshes do not need a retained snapshot or retry cursor.
		if _, err = tx.ExecContext(ctx, "DELETE FROM library_snapshots WHERE id=?", snapshot); err != nil {
			return page, err
		}
	}
	return page, tx.Commit()
}
