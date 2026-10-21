package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writebackhost"
)

func writebackFence(ctx context.Context, tx *sql.Tx, owner, source, except string) error {
	var count int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM writeback_jobs WHERE owner=? AND source_id=? AND id!=? AND state IN ('running','recovery')", owner, source, except).Scan(&count)
	if err == nil && count > 0 {
		return domain.Fail("CONFLICT", "Source has an active or unrecovered writeback")
	}
	return err
}

func validateWritebackWork(ctx context.Context, tx *sql.Tx, w writebackhost.Work, size int64, target writeback.Target, revision int64) error {
	if w.Target != target || w.Job.Request.ExpectedRevision != revision {
		return domain.Fail("CONFLICT", "Writeback execution binding changed")
	}
	t, err := writebackTrack(ctx, tx, w.Job.Owner, target)
	if err != nil {
		return err
	}
	if t.Size != size || t.ExternalID != w.ExternalID {
		return domain.Fail("RESOURCE_CHANGED", "Writeback source identity changed")
	}
	objects, _, err := writebackObjects(ctx, tx, w.Job.Owner, w.Job.Request)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(objects, w.Objects) {
		return domain.Fail("CONFLICT", "Writeback selected objects changed")
	}
	return writebackFence(ctx, tx, w.Job.Owner, target.SourceTrackID, w.Job.ID)
}

func (s *Store) BeginWriteback(ctx context.Context, owner, id string, target writeback.Target, revision int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w, size, err := readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return err
	}
	if w.Job.Status != "queued" && w.Job.Status != "running" && w.Job.Status != "recovery" {
		return domain.Fail("CONFLICT", "Writeback job is not active")
	}
	if err = validateWritebackWork(ctx, tx, w, size, target, revision); err != nil {
		return err
	}
	if w.Job.Status == "queued" {
		if _, err = tx.ExecContext(ctx, "UPDATE writeback_jobs SET state='running',error=NULL,updated_at=? WHERE owner=? AND id=?", time.Now().UTC().Format(time.RFC3339Nano), owner, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CheckWriteback(ctx context.Context, owner, id string, target writeback.Target, revision int64) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	w, size, err := readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return false, err
	}
	if w.Target != target || w.Job.Request.ExpectedRevision != revision {
		return false, domain.Fail("CONFLICT", "Writeback execution binding changed")
	}
	if w.Job.Status == "succeeded" && w.Job.Result != nil {
		return true, nil
	}
	if w.Job.Status != "running" && w.Job.Status != "recovery" {
		return false, domain.Fail("CONFLICT", "Writeback is not running or recovering")
	}
	return false, validateWritebackWork(ctx, tx, w, size, target, revision)
}

func setWritebackState(ctx context.Context, tx *sql.Tx, owner, id, state, code string) error {
	var raw []byte
	if code != "" {
		raw, _ = json.Marshal(domain.Error{Code: code, Message: "Writeback operation requires attention"})
	}
	_, err := tx.ExecContext(ctx, "UPDATE writeback_jobs SET state=?,error=?,updated_at=? WHERE owner=? AND id=?", state, raw, time.Now().UTC().Format(time.RFC3339Nano), owner, id)
	return err
}

func (s *Store) SetWritebackState(ctx context.Context, owner, id, state, code string) error {
	if state != "queued" && state != "running" && state != "recovery" && state != "failed" && state != "succeeded" && state != "cancelled" {
		return domain.Fail("INVALID_ARGUMENT", "Invalid writeback state")
	}
	if len(code) > 128 || strings.Trim(code, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_0123456789") != "" {
		return domain.Fail("INVALID_ARGUMENT", "Invalid writeback failure code")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	w, _, err := readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return err
	}
	old := w.Job.Status
	if old == state {
		return nil
	}
	allowed := state == "failed" && (old == "queued" || old == "running" || old == "recovery") || state == "recovery" && (old == "running" || old == "recovery") || state == "queued" && old == "running"
	if !allowed {
		return domain.Fail("CONFLICT", "Invalid writeback transition")
	}
	if err = setWritebackState(ctx, tx, owner, id, state, code); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) DeferWriteback(ctx context.Context, owner, id string) error {
	result, err := s.DB.ExecContext(ctx, "UPDATE writeback_jobs SET updated_at=? WHERE owner=? AND id=? AND state='queued'", time.Now().UTC().Format(time.RFC3339Nano), owner, id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.Fail("CONFLICT", "Only queued writeback jobs can be deferred")
	}
	return nil
}

func (s *Store) CancelWriteback(ctx context.Context, owner, id string) (writebackhost.Job, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return writebackhost.Job{}, err
	}
	defer tx.Rollback()
	w, _, err := readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return w.Job, err
	}
	if w.Job.Status == "cancelled" {
		return w.Job, nil
	}
	if w.Job.Status != "queued" {
		return w.Job, domain.Fail("CONFLICT", "Only queued writeback jobs can be cancelled")
	}
	if err = setWritebackState(ctx, tx, owner, id, "cancelled", "CANCELED"); err != nil {
		return w.Job, err
	}
	w, _, err = readWriteback(tx.QueryRowContext(ctx, "SELECT "+writebackColumns+" FROM writeback_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return w.Job, err
	}
	return w.Job, tx.Commit()
}

func (s *Store) RecoverWritebackQueue(ctx context.Context, pending map[string]bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT id,owner FROM writeback_jobs WHERE state='running'")
	if err != nil {
		return err
	}
	var jobs [][2]string
	for rows.Next() {
		var j [2]string
		if err = rows.Scan(&j[0], &j[1]); err != nil {
			rows.Close()
			return err
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		state := "queued"
		if pending[j[0]] {
			state = "recovery"
		}
		if err = setWritebackState(ctx, tx, j[1], j[0], state, ""); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var _ writebackhost.Repository = (*Store)(nil)
