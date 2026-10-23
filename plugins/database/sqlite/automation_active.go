package sqlite

import (
	"context"
	"database/sql"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// 自动任务排队与应用动作必须在同一事务里核对父扫描和取消意图。
func activeAutomation(ctx context.Context, tx *sql.Tx, id string) error {
	var state, scanState string
	var cancelled, current bool
	err := tx.QueryRowContext(ctx, `SELECT a.state,j.state,
 EXISTS(SELECT 1 FROM refresh_task_cancellations c WHERE c.job_id=j.id),
 j.generation=a.scan_generation AND j.plan_digest=a.plan_digest
 FROM metadata_automation a JOIN jobs j ON j.id=a.scan_job_id AND j.owner=a.owner WHERE a.id=?`, id).Scan(&state, &scanState, &cancelled, &current)
	if err != nil {
		return err
	}
	if cancelled {
		return domain.Fail("CANCELED", "Metadata refresh was cancelled")
	}
	if !current || scanState != "succeeded" || (state != "queued" && state != "preparing" && state != "running") {
		return domain.Fail("CONFLICT", "Automatic metadata task is no longer active")
	}
	return nil
}
