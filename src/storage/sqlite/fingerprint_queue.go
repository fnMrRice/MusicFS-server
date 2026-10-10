package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
)

type fingerprintRecord struct {
	Job           identification.FingerprintJob
	Owner         string
	Generation    int64
	Automatic     bool
	FileSizeBytes int64
	ResultID      string
}

func readFingerprint(row rowScanner) (identification.FingerprintJob, error) {
	var raw []byte
	var r fingerprintRecord
	if err := row.Scan(&raw); err != nil {
		return r.Job, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r.Job, err
	}
	j := r.Job
	j.Owner, j.Generation, j.Automatic, j.FileSizeBytes, j.ResultID = r.Owner, r.Generation, r.Automatic, r.FileSizeBytes, r.ResultID
	return j, nil
}

func saveFingerprint(ctx context.Context, tx *sql.Tx, j *identification.FingerprintJob, insert bool) error {
	j.UpdatedAt = time.Now().UTC()
	persisted := *j
	persisted.Result = nil
	raw, err := json.Marshal(fingerprintRecord{persisted, j.Owner, j.Generation, j.Automatic, j.FileSizeBytes, j.ResultID})
	if err != nil {
		return err
	}
	digest, err := j.FingerprintTarget.Digest()
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO fingerprint_jobs(id,owner,asset_id,generation,state,automatic,request_digest,request_start_ms,request_duration_ms,config_revision,result_id,document,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)", j.ID, j.Owner, j.AssetID, j.Generation, j.Status, j.Automatic, digest, j.StartMs, j.DurationMs, j.ExtractorIdentity.ConfigRevision, j.ResultID, raw, j.CreatedAt.Format(time.RFC3339Nano), j.UpdatedAt.Format(time.RFC3339Nano))
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE fingerprint_jobs SET generation=?,state=?,automatic=?,result_id=?,document=?,updated_at=? WHERE id=? AND owner=?", j.Generation, j.Status, j.Automatic, j.ResultID, raw, j.UpdatedAt.Format(time.RFC3339Nano), j.ID, j.Owner)
	}
	return err
}

func currentFingerprintTarget(ctx context.Context, tx *sql.Tx, owner string, target identification.FingerprintTarget) error {
	if err := target.Validate(); err != nil {
		return err
	}
	a, err := currentBinding(ctx, tx, owner, target.HashTarget.Binding())
	if err != nil {
		return err
	}
	if a.FileSizeBytes == nil || *a.FileSizeBytes != target.FileSizeBytes {
		return identification.Invalid()
	}
	return nil
}

func (s *Store) QueueFingerprint(ctx context.Context, owner string, target identification.FingerprintTarget, key string, automatic bool) (identification.FingerprintJob, bool, error) {
	var j identification.FingerprintJob
	if owner == "" || len(key) < 8 || len(key) > 128 {
		return j, false, identification.Invalid()
	}
	digest, err := target.Digest()
	if err != nil {
		return j, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return j, false, err
	}
	defer tx.Rollback()
	if err = currentFingerprintTarget(ctx, tx, owner, target); err != nil {
		return j, false, err
	}
	route := "fingerprint/" + target.SourceTrackID
	var prior, expires string
	var result []byte
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&prior, &result, &expires)
	if err == nil {
		if prior != digest {
			return j, false, domain.Fail("CONFLICT", "Idempotency key was used with another fingerprint request")
		}
		until, e := time.Parse(time.RFC3339Nano, expires)
		if e != nil || time.Now().After(until) {
			return j, false, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new operation")
		}
		j, err = readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE id=? AND owner=?", string(result), owner))
		if err == nil {
			err = hydrateFingerprint(ctx, tx, &j)
		}
		return j, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return j, false, err
	}
	j, err = readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE owner=? AND asset_id=? AND request_digest=? AND state IN ('queued','running') LIMIT 1", owner, target.AssetID, digest))
	merged := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM fingerprint_jobs WHERE state IN ('queued','running')").Scan(&count); err != nil {
			return j, false, err
		}
		if count >= 1024 {
			return j, false, domain.Fail("RATE_LIMITED", "Fingerprint queue is full")
		}
		now := time.Now().UTC()
		j = identification.FingerprintJob{ID: domain.NewID(), Owner: owner, FingerprintTarget: target, Generation: 1, Automatic: automatic, Status: "queued", CreatedAt: now, UpdatedAt: now}
		index, e := readFingerprintIndex(tx.QueryRowContext(ctx, "SELECT document,current FROM fingerprint_indexes WHERE owner=? AND asset_id=? AND request_digest=? AND current=1", owner, target.AssetID, digest))
		if e == nil {
			j.Status, j.ResultID, j.Result = "succeeded", index.ID, &index
		} else if !errors.Is(e, sql.ErrNoRows) {
			return j, false, e
		}
		if err = saveFingerprint(ctx, tx, &j, true); err != nil {
			return j, false, err
		}
	} else if err != nil {
		return j, false, err
	} else if !automatic && j.Automatic {
		j.Automatic = false
		if err = saveFingerprint(ctx, tx, &j, false); err != nil {
			return j, false, err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency(owner,route,key,digest,result,expires_at) VALUES(?,?,?,?,?,?)", owner, route, key, digest, []byte(j.ID), time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return j, false, err
	}
	return j, merged, tx.Commit()
}

func (s *Store) GetFingerprintJob(ctx context.Context, owner, id string) (identification.FingerprintJob, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return identification.FingerprintJob{}, err
	}
	defer tx.Rollback()
	j, err := readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Fingerprint job not found")
	}
	if err == nil {
		err = hydrateFingerprint(ctx, tx, &j)
	}
	return j, err
}

func (s *Store) ClaimFingerprint(ctx context.Context) (identification.FingerprintJob, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return identification.FingerprintJob{}, err
	}
	defer tx.Rollback()
	j, err := readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE state='queued' ORDER BY automatic,created_at,id LIMIT 1"))
	if err != nil {
		return j, err
	}
	j.Status = "running"
	if err = currentFingerprintTarget(ctx, tx, j.Owner, j.FingerprintTarget); err != nil {
		if !fingerprintStale(err) {
			return j, err
		}
		j.Status = "superseded"
		j.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Fingerprint target is no longer current"}
	}
	if err = saveFingerprint(ctx, tx, &j, false); err != nil {
		return j, err
	}
	if err = tx.Commit(); err != nil {
		return j, err
	}
	if j.Status != "running" {
		return j, domain.Fail("RESOURCE_CHANGED", "Fingerprint target is no longer current")
	}
	return j, nil
}
