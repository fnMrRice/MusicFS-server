package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func readAsset(ctx context.Context, tx *sql.Tx, owner, id string) (identification.Asset, error) {
	var a identification.Asset
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM source_assets WHERE id=? AND owner=?", id, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return a, domain.Fail("NOT_FOUND", "Asset not found")
	}
	if err != nil {
		return a, err
	}
	err = json.Unmarshal(raw, &a)
	return a, err
}

func (s *Store) Asset(ctx context.Context, owner, id string) (identification.Asset, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return identification.Asset{}, err
	}
	defer tx.Rollback()
	return readAsset(ctx, tx, owner, id)
}

func saveAsset(ctx context.Context, tx *sql.Tx, owner string, a identification.Asset) error {
	var raw []byte
	rows, err := tx.QueryContext(ctx, "SELECT document FROM asset_evidence WHERE owner=? AND asset_id=? AND invalidated_at IS NULL ORDER BY rowid", owner, a.ID)
	if err != nil {
		return err
	}
	evidence := []identification.Evidence{}
	for rows.Next() {
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var e identification.Evidence
		if err := json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return err
		}
		evidence = append(evidence, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	a.Project(evidence)
	if err := reconcileHashConflict(ctx, tx, owner, &a); err != nil {
		return err
	}
	raw, err = json.Marshal(a)
	if err != nil {
		return err
	}
	var digest []byte
	if a.FileSHA512 != nil {
		digest, err = hex.DecodeString(*a.FileSHA512)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "UPDATE source_assets SET document=?,current=?,file_sha512=? WHERE id=? AND owner=?", raw, a.Current, digest, a.ID, owner)
	return err
}

// Updating the track document creates a new library revision. Existing cursor
// snapshots remain immutable and retain the evidence that they originally saw.
func publishAssetView(ctx context.Context, tx *sql.Tx, owner string, a identification.Asset) error {
	if err := saveAsset(ctx, tx, owner, a); err != nil {
		return err
	}
	a, err := readAsset(ctx, tx, owner, a.ID)
	if err != nil {
		return err
	}
	var raw []byte
	err = tx.QueryRowContext(ctx, "SELECT document FROM tracks WHERE owner=? AND id=? AND asset_id=? AND version=?", owner, a.SourceTrackID, a.ID, a.Version).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("CONFLICT", "Asset version is no longer current")
	}
	if err != nil {
		return err
	}
	var track library.Track
	if err := json.Unmarshal(raw, &track); err != nil {
		return err
	}
	track.Asset, track.BitrateBps = &a, a.BitrateBps
	raw, err = json.Marshal(track)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE tracks SET document=? WHERE id=? AND owner=?", raw, track.ID, owner); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1")
	return err
}

func invalidateEvidence(ctx context.Context, tx *sql.Tx, owner, asset, kind, origin string) error {
	query := "SELECT document FROM asset_evidence WHERE owner=? AND asset_id=? AND invalidated_at IS NULL"
	args := []any{owner, asset}
	if kind != "" {
		query += " AND kind=? AND origin=?"
		args = append(args, kind, origin)
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	var all []identification.Evidence
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		var e identification.Evidence
		if err := json.Unmarshal(raw, &e); err != nil {
			rows.Close()
			return err
		}
		all = append(all, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, e := range all {
		e.Status, e.InvalidatedAt = "invalidated", &now
		raw, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE asset_evidence SET status='invalidated',invalidated_at=?,document=? WHERE id=?", now.Format(time.RFC3339Nano), raw, e.ID); err != nil {
			return err
		}
	}
	return nil
}

func invalidateAsset(ctx context.Context, tx *sql.Tx, owner, id string) error {
	a, err := readAsset(ctx, tx, owner, id)
	if err != nil {
		return err
	}
	if err := invalidateEvidence(ctx, tx, owner, id, "", ""); err != nil {
		return err
	}
	a.Current = false
	if _, err := tx.ExecContext(ctx, "UPDATE fingerprint_indexes SET current=0 WHERE owner=? AND asset_id=?", owner, id); err != nil {
		return err
	}
	if err := saveAsset(ctx, tx, owner, a); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE source_assets SET invalidated_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), id)
	return err
}
