package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

// ScanStatus 读取已有任务，优先展示当前执行或等待中的任务，不触发扫描。
func (s *Store) ScanStatus(ctx context.Context, owner, provider string) (jobs.ScanStatus, error) {
	job, err := readJob(s.DB.QueryRowContext(ctx, "SELECT "+jobColumns+` FROM jobs
WHERE owner=? AND target=?
ORDER BY CASE state WHEN 'running' THEN 0 WHEN 'retry_wait' THEN 1 WHEN 'queued' THEN 2 ELSE 3 END,
CASE WHEN state IN ('running','retry_wait','queued') THEN rtrim(created_at,'Z') END,
CASE WHEN state IN ('running','retry_wait','queued') THEN id END,
rtrim(updated_at,'Z') DESC,id DESC LIMIT 1`, owner, provider))
	if errors.Is(err, sql.ErrNoRows) {
		return jobs.ScanStatus{}, nil
	}
	if err != nil {
		return jobs.ScanStatus{}, err
	}
	return jobs.ScanStatus{Job: &job}, nil
}
