-- 分析器身份字段变更会改变证据摘要；旧明细不可冒充新身份，保留原文供审计。
CREATE TABLE retired_fingerprint_evidence (
 kind TEXT NOT NULL CHECK(kind IN ('job','index')),
 id TEXT NOT NULL,
 owner TEXT NOT NULL,
 document BLOB NOT NULL,
 retired_at TEXT NOT NULL,
 reason TEXT NOT NULL,
 PRIMARY KEY(kind,id)
);
INSERT INTO retired_fingerprint_evidence
 SELECT 'job',id,owner,document,strftime('%Y-%m-%dT%H:%M:%fZ','now'),'analyzer-identity-v2' FROM fingerprint_jobs;
INSERT INTO retired_fingerprint_evidence
 SELECT 'index',id,owner,document,strftime('%Y-%m-%dT%H:%M:%fZ','now'),'analyzer-identity-v2' FROM fingerprint_indexes;

-- 撤销派生证据和未完成发布，GC 继续遵守活动备份等引用保护。
UPDATE matching_candidates SET status='stale'
 WHERE status='pending' AND EXISTS(
 SELECT 1 FROM json_each(matching_candidates.document,'$.Candidate.reasons')
 WHERE json_extract(value,'$.kind')='fingerprintSimilar');
DELETE FROM fingerprint_indexes;
DELETE FROM fingerprint_jobs;
DELETE FROM idempotency WHERE substr(route,1,12)='fingerprint/';
UPDATE media_publications
 SET status='aborted',
     document=json_set(document,'$.Status','aborted','$.UpdatedAt',strftime('%Y-%m-%dT%H:%M:%fZ','now')),
     updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE purpose='fingerprint';
