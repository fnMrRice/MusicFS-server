package sqlite

import (
	"database/sql"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/jobs"
)

func readJobTimes(j *jobs.Job, created, updated string, next, last sql.NullString) error {
	if j.AttemptCount < 0 || j.AttemptCount > jobs.MaxAttempts || j.MaxAttempts != jobs.MaxAttempts {
		return errors.New("invalid persisted job retry budget")
	}
	var err error
	if j.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return err
	}
	if j.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return err
	}
	j.CreatedAt, j.UpdatedAt = j.CreatedAt.UTC(), j.UpdatedAt.UTC()
	for _, item := range []struct {
		raw  sql.NullString
		dest **time.Time
	}{{next, &j.NextAttemptAt}, {last, &j.LastAttemptAt}} {
		if item.raw.Valid {
			v, e := time.Parse(time.RFC3339Nano, item.raw.String)
			if e != nil {
				return e
			}
			v = v.UTC()
			*item.dest = &v
		}
	}
	if j.AttemptCount > 0 {
		j.RetryCount = j.AttemptCount - 1
	}
	return nil
}
