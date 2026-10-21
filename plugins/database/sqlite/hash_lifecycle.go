package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

func activeHash(ctx context.Context, tx *sql.Tx, j identification.HashJob) (identification.HashJob, error) {
	current, err := readHash(tx.QueryRowContext(ctx, "SELECT document FROM hash_jobs WHERE id=? AND owner=?", j.ID, j.Owner))
	if err != nil {
		return current, err
	}
	if current.Generation != j.Generation || current.Status != "running" || current.HashTarget != j.HashTarget || !reflect.DeepEqual(current.Ticket, j.Ticket) {
		return current, domain.Fail("CONFLICT", "Hash execution generation is no longer active")
	}
	return current, nil
}

func hashFailure(code string) identification.Result {
	return identification.Result{Status: "failed", BitrateMode: "unknown", FailureCode: &code}
}

func settleHash(ctx context.Context, tx *sql.Tx, j *identification.HashJob, result identification.Result) error {
	if err := publishEvidenceResult(ctx, tx, j.Owner, j.Ticket, result); err != nil {
		var known *domain.Error
		if errors.As(err, &known) && (known.Code == "CONFLICT" || known.Code == "NOT_FOUND") {
			j.Status = "superseded"
			j.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Hash target or evidence generation is no longer current"}
			return nil
		}
		return err
	}
	return nil
}

func (s *Store) FinishHash(ctx context.Context, j identification.HashJob, result identification.Result, completed int64) (finishErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.storage.sqlite", "")
	finish := pluginlog.Operation(ctx, "hash.result.persist")
	defer func() { finish(finishErr) }()
	if completed < 0 || completed > j.FileSizeBytes || result.Status == "verified" && completed != j.FileSizeBytes {
		return identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err = activeHash(ctx, tx, j)
	if err != nil {
		return err
	}
	if completed < 0 || completed > j.FileSizeBytes || result.Status == "verified" && completed != j.FileSizeBytes {
		return identification.Invalid()
	}
	j.Status = "succeeded"
	j.Progress.CompletedBytes = completed
	j.Error = nil
	if result.Status != "verified" {
		if result.Status != "failed" && result.Status != "unavailable" {
			return identification.Invalid()
		}
		j.Status = result.Status
		if result.FailureCode == nil {
			return identification.Invalid()
		}
		j.Error = &domain.Error{Code: *result.FailureCode, Message: "Complete file verification did not succeed"}
	}
	if err := settleHash(ctx, tx, &j, result); err != nil {
		return err
	}
	if err := saveHash(ctx, tx, &j, false); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CancelHash(ctx context.Context, owner, id string) (identification.HashJob, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return identification.HashJob{}, err
	}
	defer tx.Rollback()
	j, err := readHash(tx.QueryRowContext(ctx, "SELECT document FROM hash_jobs WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Hash job not found")
	}
	if err != nil {
		return j, err
	}
	if j.Status != "queued" && j.Status != "running" {
		return j, nil
	}
	j.Status = "cancelled"
	j.Error = &domain.Error{Code: "CANCELED", Message: "Hash task cancelled"}
	if err := settleHash(ctx, tx, &j, hashFailure("CANCELED")); err != nil {
		return j, err
	}
	if err := saveHash(ctx, tx, &j, false); err != nil {
		return j, err
	}
	return j, tx.Commit()
}

func (s *Store) HashProgress(ctx context.Context, j identification.HashJob, completed int64) error {
	if completed < 0 || completed > j.FileSizeBytes {
		return identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err = activeHash(ctx, tx, j)
	if err != nil {
		return err
	}
	if completed < 0 || completed > j.FileSizeBytes {
		return identification.Invalid()
	}
	if completed < j.Progress.CompletedBytes {
		return identification.Invalid()
	}
	j.Progress.CompletedBytes = completed
	if err := saveHash(ctx, tx, &j, false); err != nil {
		return err
	}
	return tx.Commit()
}

// Recovery replaces execution and evidence generations in one transaction. A
// process that retained an earlier ticket can never publish after recovery.
func (s *Store) RecoverHashes(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT document FROM hash_jobs WHERE state='running'")
	if err != nil {
		return err
	}
	var jobs []identification.HashJob
	for rows.Next() {
		j, e := readHash(rows)
		if e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		j.Generation++
		j.Progress.CompletedBytes = 0
		j.Status = "queued"
		j.Ticket, err = hashTicket(ctx, tx, j.Owner, j.HashTarget)
		if err != nil {
			var known *domain.Error
			if !errors.As(err, &known) || (known.Code != "CONFLICT" && known.Code != "NOT_FOUND") {
				return err
			}
			j.Status = "superseded"
			j.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Hash target is no longer current"}
		}
		if err := saveHash(ctx, tx, &j, false); err != nil {
			return err
		}
	}
	return tx.Commit()
}
