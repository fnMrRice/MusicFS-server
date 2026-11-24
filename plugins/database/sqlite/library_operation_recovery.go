package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

// 恢复时保留排队任务。运行中的合并可能已提交关联事务，从同一事务的幂等收据还原结果。
func (s *Store) RecoverOperations(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT o.id,
 json_extract(i.result,'$.mergedGroups'),json_extract(i.result,'$.mergedSources'),json_extract(i.result,'$.skippedGroups')
 FROM library_operations o JOIN idempotency i ON i.owner=o.owner AND i.key=o.id AND i.route='matching/bulk-merge'
 WHERE o.kind='bulkMerge' AND o.state='running'`)
	if err != nil {
		return err
	}
	committed := map[string]tasks.Result{}
	for rows.Next() {
		var id string
		var counts tasks.Result
		if err = rows.Scan(&id, &counts.MergedGroups, &counts.MergedSources, &counts.SkippedGroups); err != nil {
			rows.Close()
			return err
		}
		committed[id] = counts
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	progress, err := json.Marshal(tasks.Progress{Total: 1, Completed: 1})
	if err != nil {
		return err
	}
	for id, counts := range committed {
		result, err := json.Marshal(counts)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE library_operations SET state='succeeded',progress=?,result=?,failure=NULL,updated_at=? WHERE id=?", string(progress), string(result), now, id); err != nil {
			return err
		}
	}
	failure, err := json.Marshal(domain.Error{Code: "TEMPORARY_FAILURE", Message: "Server stopped before the operation completed"})
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE library_operations SET state='failed',failure=?,updated_at=? WHERE state='running'", string(failure), now); err != nil {
		return err
	}
	return tx.Commit()
}
