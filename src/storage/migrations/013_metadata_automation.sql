CREATE TABLE metadata_automation (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,scan_job_id TEXT NOT NULL REFERENCES jobs(id),scan_generation INTEGER NOT NULL,
 plan_digest TEXT NOT NULL,root_id TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL,asset_version TEXT NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('automatic','importLyrics')),
 state TEXT NOT NULL CHECK(state IN ('queued','preparing','running','completed','skipped','failed')),
 policy BLOB,job_id TEXT REFERENCES enrichment_jobs(id),failure_code TEXT,created_at TEXT NOT NULL,
 UNIQUE(scan_job_id,scan_generation,source_id,reason)
);
CREATE INDEX metadata_automation_queue ON metadata_automation(state,created_at,id);
CREATE INDEX metadata_automation_scan ON metadata_automation(owner,scan_job_id,id);
CREATE TABLE metadata_automation_actions (
 trigger_id TEXT NOT NULL REFERENCES metadata_automation(id),field TEXT NOT NULL,language TEXT NOT NULL,
 expected_revision INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(trigger_id,field,language)
);
