ALTER TABLE jobs ADD COLUMN attempt_count INTEGER NOT NULL DEFAULT 0 CHECK(attempt_count>=0 AND attempt_count<=3);
ALTER TABLE jobs ADD COLUMN max_attempts INTEGER NOT NULL DEFAULT 3 CHECK(max_attempts=3);
ALTER TABLE jobs ADD COLUMN next_attempt_at TEXT;
ALTER TABLE jobs ADD COLUMN last_attempt_at TEXT;
DROP INDEX jobs_active_plan;
CREATE UNIQUE INDEX jobs_active_plan ON jobs(owner,target,plan_digest) WHERE state IN ('queued','running','retry_wait');
CREATE INDEX jobs_due ON jobs(state,next_attempt_at,created_at);
