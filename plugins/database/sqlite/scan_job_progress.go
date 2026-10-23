package sqlite

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

// UpdateProgress 只更新当前执行代次的发现数量，不发布曲库或改变任务完成状态。
func (s *Store) UpdateProgress(ctx context.Context, given jobs.Job, observed int) error {
	if observed < 0 || observed > 50000 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid observed count")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := activeScan(ctx, tx, given)
	if err != nil {
		return err
	}
	if observed < j.Progress.Observed {
		return domain.Fail("CONFLICT", "Scan progress cannot move backwards")
	}
	if observed == j.Progress.Observed {
		return nil
	}
	_, err = tx.ExecContext(ctx, "UPDATE jobs SET observed=?,updated_at=? WHERE id=? AND owner=?", observed, time.Now().UTC().Format(time.RFC3339Nano), j.ID, j.Owner)
	if err != nil {
		return err
	}
	return tx.Commit()
}
