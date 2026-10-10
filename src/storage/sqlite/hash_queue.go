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

type hashRecord struct {
	Job        identification.HashJob
	Owner      string
	Ticket     identification.Ticket
	Generation int64
	Automatic  bool
}

func readHash(row rowScanner) (identification.HashJob, error) {
	var raw []byte
	var record hashRecord
	if err := row.Scan(&raw); err != nil {
		return record.Job, err
	}
	if err := json.Unmarshal(raw, &record); err != nil {
		return record.Job, err
	}
	j := record.Job
	j.Owner, j.Ticket, j.Generation, j.Automatic = record.Owner, record.Ticket, record.Generation, record.Automatic
	j.FileSizeBytes = j.Progress.TotalBytes
	return j, nil
}

func saveHash(ctx context.Context, tx *sql.Tx, j *identification.HashJob, insert bool) error {
	j.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(hashRecord{Job: *j, Owner: j.Owner, Ticket: j.Ticket, Generation: j.Generation, Automatic: j.Automatic})
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO hash_jobs(id,owner,asset_id,generation,state,automatic,document,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)", j.ID, j.Owner, j.AssetID, j.Generation, j.Status, j.Automatic, raw, j.CreatedAt.Format(time.RFC3339Nano), j.UpdatedAt.Format(time.RFC3339Nano))
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE hash_jobs SET generation=?,state=?,automatic=?,document=?,updated_at=? WHERE id=? AND owner=?", j.Generation, j.Status, j.Automatic, raw, j.UpdatedAt.Format(time.RFC3339Nano), j.ID, j.Owner)
	}
	return err
}

func hashTicket(ctx context.Context, tx *sql.Tx, owner string, target identification.HashTarget) (identification.Ticket, error) {
	b := target.Binding()
	a, err := currentBinding(ctx, tx, owner, b)
	if err != nil {
		return identification.Ticket{}, err
	}
	if a.FileSizeBytes == nil || *a.FileSizeBytes != target.FileSizeBytes {
		return identification.Ticket{}, identification.Invalid()
	}
	e, err := insertEvidence(ctx, tx, owner, a, b.Spec, identification.Result{Status: "pending", BitrateMode: "unknown"})
	if err != nil {
		return identification.Ticket{}, err
	}
	if err = publishAssetView(ctx, tx, owner, a); err != nil {
		return identification.Ticket{}, err
	}
	return identification.Ticket{ID: e.ID, Generation: e.Generation, Binding: b}, nil
}

func (s *Store) QueueHash(ctx context.Context, owner string, target identification.HashTarget, key string, automatic bool) (identification.HashJob, bool, error) {
	var j identification.HashJob
	if len(key) < 8 || len(key) > 128 || target.FileSizeBytes < 0 {
		return j, false, identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return j, false, err
	}
	defer tx.Rollback()
	a, err := currentBinding(ctx, tx, owner, target.Binding())
	if err != nil {
		return j, false, err
	}
	if a.FileSizeBytes == nil || *a.FileSizeBytes != target.FileSizeBytes {
		return j, false, identification.Invalid()
	}
	route, digest := "hash/"+target.SourceTrackID, target.AssetID+"/"+target.AssetVersion
	var previous, expires string
	var result []byte
	err = tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&previous, &result, &expires)
	if err == nil {
		if previous != digest {
			return j, false, domain.Fail("CONFLICT", "Idempotency key was used with another asset version")
		}
		expiry, e := time.Parse(time.RFC3339Nano, expires)
		if e != nil || time.Now().After(expiry) {
			return j, false, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new operation")
		}
		j, err = readHash(tx.QueryRowContext(ctx, "SELECT document FROM hash_jobs WHERE id=? AND owner=?", string(result), owner))
		return j, true, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return j, false, err
	}
	j, err = readHash(tx.QueryRowContext(ctx, "SELECT document FROM hash_jobs WHERE owner=? AND asset_id=? AND state IN ('queued','running') LIMIT 1", owner, target.AssetID))
	merged := err == nil
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM hash_jobs WHERE state IN ('queued','running')").Scan(&count); err != nil {
			return j, false, err
		}
		if count >= 1024 {
			return j, false, domain.Fail("RATE_LIMITED", "Hash queue is full")
		}
		now := time.Now().UTC()
		j = identification.HashJob{ID: domain.NewID(), HashTarget: target, Owner: owner, Generation: 1, Automatic: automatic, Status: "queued", Progress: identification.HashProgress{TotalBytes: target.FileSizeBytes}, CreatedAt: now, UpdatedAt: now}
		if a.HashStatus == "verified" {
			j.Status = "succeeded"
			j.Progress.CompletedBytes = target.FileSizeBytes
		} else {
			j.Ticket, err = hashTicket(ctx, tx, owner, target)
			if err != nil {
				return j, false, err
			}
		}
		if err = saveHash(ctx, tx, &j, true); err != nil {
			return j, false, err
		}
	} else if err != nil {
		return j, false, err
	} else if !automatic && j.Automatic {
		j.Automatic = false
		if err = saveHash(ctx, tx, &j, false); err != nil {
			return j, false, err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency(owner,route,key,digest,result,expires_at) VALUES(?,?,?,?,?,?)", owner, route, key, digest, []byte(j.ID), time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return j, false, err
	}
	return j, merged, tx.Commit()
}

func (s *Store) GetHashJob(ctx context.Context, owner, id string) (identification.HashJob, error) {
	j, err := readHash(s.DB.QueryRowContext(ctx, "SELECT document FROM hash_jobs WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Hash job not found")
	}
	return j, err
}

func (s *Store) ClaimHash(ctx context.Context) (identification.HashJob, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return identification.HashJob{}, err
	}
	defer tx.Rollback()
	j, err := readHash(tx.QueryRowContext(ctx, "SELECT document FROM hash_jobs WHERE state='queued' ORDER BY automatic,created_at,id LIMIT 1"))
	if err != nil {
		return j, err
	}
	j.Status = "running"
	if err = saveHash(ctx, tx, &j, false); err != nil {
		return j, err
	}
	return j, tx.Commit()
}

func (s *Store) HashCandidates(ctx context.Context, scope identification.HashScope, after string, limit int) ([]identification.HashTarget, error) {
	if limit < 1 || limit > 64 {
		return nil, identification.Invalid()
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT t.id,t.asset_id,t.version,t.size FROM tracks t JOIN source_assets a ON a.id=t.asset_id WHERE t.owner=? AND t.provider=? AND t.id>? AND a.current=1 AND a.file_sha512 IS NULL AND NOT EXISTS(SELECT 1 FROM hash_jobs h WHERE h.owner=t.owner AND h.asset_id=a.id) ORDER BY t.id LIMIT ?", scope.Owner, scope.ProviderInstanceID, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []identification.HashTarget{}
	for rows.Next() {
		var target identification.HashTarget
		if err := rows.Scan(&target.SourceTrackID, &target.AssetID, &target.AssetVersion, &target.FileSizeBytes); err != nil {
			return nil, err
		}
		items = append(items, target)
	}
	return items, rows.Err()
}
