package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

var _ workpackages.Repository = (*Store)(nil)

func packageOperation(owner, work, preview, key string) (string, string, error) {
	if !works.ValidID(owner) || !works.ValidID(work) || !works.ValidID(preview) || !works.ValidID(key) || len(key) < 8 {
		return "", "", domain.Fail("INVALID_ARGUMENT", "Invalid work package operation")
	}
	raw, _ := json.Marshal(struct {
		PreviewID string `json:"previewId"`
	}{preview})
	digest := sha256.Sum256(raw)
	return "works/" + work + "/packages/import", hex.EncodeToString(digest[:]), nil
}

func readWorkPackage(ctx context.Context, tx *sql.Tx, owner, work, id string) (workpackages.Package, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM work_packages WHERE owner=? AND work_id=? AND id=?", owner, work, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return workpackages.Package{}, domain.Fail("NOT_FOUND", "Work package not found")
	}
	if err != nil {
		return workpackages.Package{}, err
	}
	return decodeWorkPackage(raw, owner)
}

func (s *Store) WorkPackage(ctx context.Context, owner, work, id string) (workpackages.Package, error) {
	if !works.ValidID(owner) || !works.ValidID(work) || !works.ValidID(id) {
		return workpackages.Package{}, domain.Fail("INVALID_ARGUMENT", "Invalid work package identifier")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return workpackages.Package{}, err
	}
	defer tx.Rollback()
	return readWorkPackage(ctx, tx, owner, work, id)
}

func (s *Store) WorkPackages(ctx context.Context, owner, work string) ([]workpackages.Package, error) {
	if !works.ValidID(owner) || !works.ValidID(work) {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid work identifier")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = readWork(ctx, tx, owner, work); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM work_packages WHERE owner=? AND work_id=? ORDER BY id", owner, work)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []workpackages.Package{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		item, err := decodeWorkPackage(raw, owner)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Store) ReplayWorkPackage(ctx context.Context, owner, work, preview, key string) (workpackages.Package, bool, error) {
	route, digest, err := packageOperation(owner, work, preview, key)
	if err != nil {
		return workpackages.Package{}, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return workpackages.Package{}, false, err
	}
	defer tx.Rollback()
	raw, found, err := contentIdempotency(ctx, tx, owner, route, key, digest)
	if err != nil || !found {
		return workpackages.Package{}, false, err
	}
	p, err := decodeWorkPackage(raw, owner)
	return p, err == nil, err
}
