package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
)

func reconcileHashConflict(ctx context.Context, tx *sql.Tx, owner string, a *identification.Asset) error {
	var claim, proof string
	if a.Current && a.ProviderSHA512 != nil && a.FileSHA512 != nil && *a.ProviderSHA512 != *a.FileSHA512 {
		for _, e := range a.Evidence {
			if e.Kind == "providerSha512" && e.Status == "reported" {
				claim = e.ID
			}
			if e.Kind == "fileSha512" && e.Status == "verified" {
				proof = e.ID
			}
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM hash_conflicts WHERE owner=? AND asset_id=? AND resolved_at IS NULL", owner, a.ID)
	if err != nil {
		return err
	}
	var previous []identification.HashConflict
	for rows.Next() {
		var raw []byte
		var c identification.HashConflict
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			rows.Close()
			return err
		}
		previous = append(previous, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	for _, c := range previous {
		if c.ProviderEvidenceID == claim && c.VerifiedEvidenceID == proof {
			copy := c
			a.HashConflict = &copy
			continue
		}
		c.ResolvedAt = &now
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE hash_conflicts SET document=?,resolved_at=? WHERE id=?", raw, now.Format(time.RFC3339Nano), c.ID); err != nil {
			return err
		}
	}
	if claim != "" && proof != "" && a.HashConflict == nil {
		c := identification.HashConflict{ID: domain.NewID(), ProviderEvidenceID: claim, VerifiedEvidenceID: proof, Reason: "PROVIDER_SHA512_MISMATCH", DetectedAt: now}
		raw, err := json.Marshal(c)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO hash_conflicts(id,owner,asset_id,provider_evidence_id,verified_evidence_id,document) VALUES(?,?,?,?,?,?)", c.ID, owner, a.ID, claim, proof, raw); err != nil {
			return err
		}
		a.HashConflict = &c
	}
	return nil
}

func (s *Store) HashConflictHistory(ctx context.Context, owner, asset, before string, limit int) ([]identification.HashConflict, error) {
	if limit < 1 || limit > 200 {
		return nil, identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = readAsset(ctx, tx, owner, asset); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM hash_conflicts WHERE owner=? AND asset_id=? AND (?='' OR id<?) ORDER BY id DESC LIMIT ?", owner, asset, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []identification.HashConflict{}
	for rows.Next() {
		var raw []byte
		var c identification.HashConflict
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

// Only verified original-file digests identify byte equality. Claims are never
// used here, and every result retains its own track and owner authorization.
func (s *Store) HashMatches(ctx context.Context, owner, asset, after string, limit int) ([]identification.HashTarget, error) {
	if limit < 1 || limit > 200 {
		return nil, identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a, err := readAsset(ctx, tx, owner, asset)
	if err != nil {
		return nil, err
	}
	items := []identification.HashTarget{}
	if !a.Current || a.HashStatus != "verified" {
		return items, nil
	}
	rows, err := tx.QueryContext(ctx, "SELECT t.id,t.asset_id,t.version,t.size FROM source_assets a JOIN tracks t ON t.asset_id=a.id WHERE a.owner=? AND a.current=1 AND a.file_sha512=(SELECT file_sha512 FROM source_assets WHERE id=? AND owner=?) AND a.id<>? AND a.id>? ORDER BY a.id LIMIT ?", owner, asset, owner, asset, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v identification.HashTarget
		if err = rows.Scan(&v.SourceTrackID, &v.AssetID, &v.AssetVersion, &v.FileSizeBytes); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}
