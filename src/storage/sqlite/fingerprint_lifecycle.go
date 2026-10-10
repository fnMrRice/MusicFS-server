package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
)

func fingerprintStale(err error) bool {
	var known *domain.Error
	return errors.As(err, &known) && (known.Code == "CONFLICT" || known.Code == "NOT_FOUND")
}

func activeFingerprint(ctx context.Context, tx *sql.Tx, given identification.FingerprintJob) (identification.FingerprintJob, error) {
	j, err := readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE id=? AND owner=?", given.ID, given.Owner))
	if err != nil {
		return j, err
	}
	if j.Generation != given.Generation || j.Status != "running" || j.FingerprintTarget != given.FingerprintTarget {
		return j, domain.Fail("CONFLICT", "Fingerprint execution is no longer active")
	}
	return j, nil
}

func (s *Store) FailFingerprint(ctx context.Context, given identification.FingerprintJob, code string) error {
	if code == "" || len(code) > 64 {
		return identification.Invalid()
	}
	for _, r := range code {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return identification.Invalid()
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := activeFingerprint(ctx, tx, given)
	if err != nil {
		return err
	}
	j.Status = "failed"
	switch code {
	case "CANCELED":
		j.Status = "cancelled"
	case "UNSUPPORTED":
		j.Status = "unavailable"
	case "RESOURCE_CHANGED":
		j.Status = "superseded"
	}
	if err = currentFingerprintTarget(ctx, tx, j.Owner, j.FingerprintTarget); err != nil {
		if !fingerprintStale(err) {
			return err
		}
		j.Status, code = "superseded", "RESOURCE_CHANGED"
	}
	j.Error = &domain.Error{Code: code, Message: "Fingerprint extraction did not complete"}
	if err = saveFingerprint(ctx, tx, &j, false); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelFingerprint(ctx context.Context, owner, id string) (identification.FingerprintJob, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return identification.FingerprintJob{}, err
	}
	defer tx.Rollback()
	j, err := readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Fingerprint job not found")
	}
	if err != nil {
		return j, err
	}
	if j.Status != "queued" && j.Status != "running" {
		err = hydrateFingerprint(ctx, tx, &j)
		return j, err
	}
	j.Status = "cancelled"
	j.Error = &domain.Error{Code: "CANCELED", Message: "Fingerprint task cancelled"}
	if err = saveFingerprint(ctx, tx, &j, false); err != nil {
		return j, err
	}
	return j, tx.Commit()
}

// Restarted tasks always receive a new generation. Publication recovery aborts
// unreferenced files first; no lost source guard can turn them into evidence.
func (s *Store) RecoverFingerprints(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT document FROM fingerprint_jobs WHERE state='running'")
	if err != nil {
		return err
	}
	var pending []identification.FingerprintJob
	for rows.Next() {
		j, e := readFingerprint(rows)
		if e != nil {
			rows.Close()
			return e
		}
		pending = append(pending, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range pending {
		j.Generation++
		j.Status = "queued"
		j.Error = nil
		if err = currentFingerprintTarget(ctx, tx, j.Owner, j.FingerprintTarget); err != nil {
			if !fingerprintStale(err) {
				return err
			}
			j.Status = "superseded"
			j.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Fingerprint target is no longer current"}
		}
		if err = saveFingerprint(ctx, tx, &j, false); err != nil {
			return err
		}
	}
	return tx.Commit()
}
