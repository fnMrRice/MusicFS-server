package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

const operationPrefix = "op-"
const operationColumns = "id,owner,kind,payload,state,progress,result,failure,created_at,updated_at"

func readOperation(row rowScanner) (tasks.Operation, error) {
	var op tasks.Operation
	t := &op.Task
	var payload, progress, result, failure []byte
	var created, updated string
	if err := row.Scan(&t.ID, &t.OwnerID, &t.Kind, &payload, &t.Status, &progress, &result, &failure, &created, &updated); err != nil {
		return op, err
	}
	op.Payload = payload
	t.RootIDs, t.Roots = []string{}, []tasks.Root{}
	t.OwnerName = t.OwnerID
	t.Cancellable, t.CancelRequested = t.Status == "queued", t.Status == "cancelled"
	switch t.Status {
	case "queued":
		t.Phase = "queued"
	case "running":
		t.Phase = "merging"
		if t.Kind == tasks.AlbumAutoScrape {
			t.Phase = "scraping"
		}
	default:
		t.Phase = "finished"
	}
	if err := json.Unmarshal(progress, &t.Progress); err != nil {
		return op, err
	}
	if err := json.Unmarshal(result, &t.Result); err != nil {
		return op, err
	}
	if len(failure) > 0 {
		if err := json.Unmarshal(failure, &t.Error); err != nil {
			return op, err
		}
	}
	var err error
	if t.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return op, err
	}
	t.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return op, err
}

func getOperation(ctx context.Context, tx *sql.Tx, owner, id string) (tasks.Operation, error) {
	op, err := readOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM library_operations WHERE id=? AND (?='' OR owner=?)", id, owner, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Task not found")
	}
	return op, err
}

func (s *Store) EnqueueOperation(ctx context.Context, owner, kind, key string, payload json.RawMessage) (tasks.Task, error) {
	if owner == "" || (kind != tasks.AlbumAutoScrape && kind != tasks.BulkMerge) || len(key) < 8 || len(key) > 128 || !json.Valid(payload) {
		return tasks.Task{}, domain.Fail("INVALID_ARGUMENT", "Invalid library operation")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return tasks.Task{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = tx.ExecContext(ctx, `INSERT INTO library_operations(id,owner,kind,request_key,payload,state,created_at,updated_at)
 VALUES(?,?,?,?,?,'queued',?,?) ON CONFLICT(owner,kind,request_key) DO NOTHING`, operationPrefix+domain.NewID(), owner, kind, key, string(payload), now, now)
	if err != nil {
		return tasks.Task{}, err
	}
	op, err := readOperation(tx.QueryRowContext(ctx, "SELECT "+operationColumns+" FROM library_operations WHERE owner=? AND kind=? AND request_key=?", owner, kind, key))
	if err != nil {
		return tasks.Task{}, err
	}
	if !bytes.Equal(op.Payload, payload) {
		return tasks.Task{}, domain.Fail("CONFLICT", "Idempotency key was used with another operation")
	}
	return op.Task, tx.Commit()
}

func (s *Store) ClaimOperation(ctx context.Context) (*tasks.Operation, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	row := s.DB.QueryRowContext(ctx, `UPDATE library_operations SET state='running',updated_at=?
 WHERE id=(SELECT id FROM library_operations WHERE state='queued' ORDER BY rtrim(created_at,'Z'),id LIMIT 1)
 AND state='queued' RETURNING `+operationColumns, now)
	op, err := readOperation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &op, nil
}

func (s *Store) UpdateOperation(ctx context.Context, id string, progress tasks.Progress, result tasks.Result) error {
	return s.writeOperation(ctx, id, "running", progress, result, nil)
}

func (s *Store) FinishOperation(ctx context.Context, id, status string, progress tasks.Progress, result tasks.Result, failure *domain.Error) error {
	if (status != "failed" && status != "succeeded") || (status == "failed") != (failure != nil) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid terminal operation state")
	}
	return s.writeOperation(ctx, id, status, progress, result, failure)
}

func (s *Store) writeOperation(ctx context.Context, id, status string, progress tasks.Progress, result tasks.Result, failure *domain.Error) error {
	p, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	r, err := json.Marshal(result)
	if err != nil {
		return err
	}
	f, err := json.Marshal(failure)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := s.DB.ExecContext(ctx, `UPDATE library_operations SET state=?,progress=?,result=?,failure=?,updated_at=? WHERE id=? AND state='running'`, status, string(p), string(r), string(f), now, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 0 {
		return err
	}
	// 终态写入可能已提交但 RPC 回应丢失；相同结果的重放不再修改时间。
	if status != "running" {
		var count int
		err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM library_operations WHERE id=? AND state=? AND progress=? AND result=? AND failure=?", id, status, string(p), string(r), string(f)).Scan(&count)
		if err != nil || count == 1 {
			return err
		}
	}
	return domain.Fail("CONFLICT", "Operation is no longer running")
}

func cancelQueuedOperation(ctx context.Context, tx *sql.Tx, owner, id string) (tasks.Task, error) {
	failure, err := json.Marshal(domain.Error{Code: "CANCELED", Message: "Operation cancelled"})
	if err != nil {
		return tasks.Task{}, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE library_operations SET state='cancelled',updated_at=?,failure=?
 WHERE id=? AND (?='' OR owner=?) AND state='queued'`, time.Now().UTC().Format(time.RFC3339Nano), string(failure), id, owner, owner)
	if err != nil {
		return tasks.Task{}, err
	}
	op, err := getOperation(ctx, tx, owner, id)
	if err != nil {
		return tasks.Task{}, err
	}
	return op.Task, tx.Commit()
}
