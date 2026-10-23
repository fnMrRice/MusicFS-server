-- 取消意图先阻断发布；资源释放确认后记录完成时间，重启恢复可完成旧进程的取消。
CREATE TABLE refresh_task_cancellations (
 job_id TEXT PRIMARY KEY REFERENCES jobs(id),
 requested_at TEXT NOT NULL,
 completed_at TEXT
);
CREATE INDEX jobs_metadata_refresh ON jobs(owner,created_at DESC,id DESC)
 WHERE json_extract(plan,'$.refreshMetadata')=1;
