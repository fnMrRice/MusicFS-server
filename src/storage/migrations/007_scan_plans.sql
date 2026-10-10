-- Discard unplanned scan task caches explicitly. No synthetic plan can prove
-- their authorized roots; library tracks, assets and evidence remain intact.
DELETE FROM idempotency WHERE substr(route,1,5)='scan/';
DELETE FROM jobs;
ALTER TABLE jobs ADD COLUMN plan BLOB NOT NULL;
ALTER TABLE jobs ADD COLUMN plan_digest TEXT NOT NULL;
CREATE UNIQUE INDEX jobs_active_plan ON jobs(owner,target,plan_digest) WHERE state IN ('queued','running');
CREATE TABLE provider_scan_leases (
 owner TEXT NOT NULL, provider TEXT NOT NULL, last_generation INTEGER NOT NULL CHECK(last_generation>=0),
 active_job_id TEXT REFERENCES jobs(id), PRIMARY KEY(owner,provider)
);
