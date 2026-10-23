-- 扩展单次扫描的元数据刷新原因，保留已有触发记录、策略、动作及外键约束。
CREATE TABLE metadata_automation_next (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,scan_job_id TEXT NOT NULL REFERENCES jobs(id),scan_generation INTEGER NOT NULL,
 plan_digest TEXT NOT NULL,root_id TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL,asset_version TEXT NOT NULL,
 reason TEXT NOT NULL CHECK(reason IN ('automatic','importLyrics','importArtwork','refreshMetadata')),
 state TEXT NOT NULL CHECK(state IN ('queued','preparing','running','completed','skipped','failed')),
 policy BLOB,job_id TEXT REFERENCES enrichment_jobs(id),failure_code TEXT,created_at TEXT NOT NULL,
 UNIQUE(scan_job_id,scan_generation,source_id,reason)
);
INSERT INTO metadata_automation_next SELECT * FROM metadata_automation;

CREATE TABLE metadata_automation_actions_next (
 trigger_id TEXT NOT NULL REFERENCES metadata_automation_next(id),field TEXT NOT NULL,language TEXT NOT NULL,
 expected_revision INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(trigger_id,field,language)
);
INSERT INTO metadata_automation_actions_next SELECT * FROM metadata_automation_actions;
DROP TABLE metadata_automation_actions;
DROP TABLE metadata_automation;
ALTER TABLE metadata_automation_next RENAME TO metadata_automation;
ALTER TABLE metadata_automation_actions_next RENAME TO metadata_automation_actions;
CREATE INDEX metadata_automation_queue ON metadata_automation(state,created_at,id);
CREATE INDEX metadata_automation_scan ON metadata_automation(owner,scan_job_id,id);
