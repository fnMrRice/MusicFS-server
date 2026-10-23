package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

// 每个目录只执行一次聚合，轮询成本不随歌曲数增加数据库往返次数。
func refreshTaskSummary(ctx context.Context, tx *sql.Tx, j jobs.Job, task *tasks.Task) error {
	var updated, code sql.NullString
	err := tx.QueryRowContext(ctx, `WITH relevant AS (
 SELECT id,state,failure_code,job_id,owner FROM metadata_automation
 WHERE owner=? AND scan_job_id=? AND scan_generation=? AND plan_digest=? AND reason='refreshMetadata'
), actions AS (
 SELECT x.trigger_id,
 SUM(CASE WHEN json_extract(x.document,'$.status')='applied' THEN 1 ELSE 0 END) AS applied,
 SUM(CASE WHEN json_extract(x.document,'$.status')='skipped' AND json_extract(x.document,'$.reason')!='noEligibleCandidate' THEN 1 ELSE 0 END) AS retained,
 SUM(CASE WHEN json_extract(x.document,'$.status')='skipped' AND json_extract(x.document,'$.reason')='noEligibleCandidate' THEN 1 ELSE 0 END) AS missing,
 SUM(CASE WHEN json_extract(x.document,'$.status')='failed' THEN 1 ELSE 0 END) AS failed
 FROM metadata_automation_actions x JOIN relevant a ON a.id=x.trigger_id GROUP BY x.trigger_id
)
 SELECT COUNT(*),COALESCE(SUM(CASE WHEN a.state IN ('completed','skipped','failed') THEN 1 ELSE 0 END),0),
 COALESCE(SUM(x.applied),0),COALESCE(SUM(x.retained),0),COALESCE(SUM(x.missing),0),
 COALESCE(SUM(CASE WHEN a.state='failed' AND COALESCE(a.failure_code,'')!='CANCELED' AND COALESCE(x.failed,0)=0 THEN 1 ELSE COALESCE(x.failed,0) END),0),
 MAX(e.updated_at),MIN(CASE WHEN a.state='failed' THEN a.failure_code ELSE NULL END)
 FROM relevant a LEFT JOIN actions x ON x.trigger_id=a.id LEFT JOIN enrichment_jobs e ON e.id=a.job_id AND e.owner=a.owner`, j.Owner, j.ID, j.Generation, j.PlanDigest).Scan(
		&task.Progress.MetadataTotal, &task.Progress.MetadataCompleted, &task.Result.Updated,
		&task.Result.Retained, &task.Result.NotFound, &task.Result.Failed, &updated, &code)
	if err != nil {
		return err
	}
	if updated.Valid {
		if stamp, err := time.Parse(time.RFC3339Nano, updated.String); err == nil && stamp.After(task.UpdatedAt) {
			task.UpdatedAt = stamp
		}
	}
	if code.Valid && task.Error == nil {
		task.Error = &domain.Error{Code: code.String, Message: "Metadata refresh failed"}
	}
	return nil
}
