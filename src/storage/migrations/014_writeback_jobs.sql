CREATE TABLE writeback_jobs (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),
 state TEXT NOT NULL CHECK(state IN ('queued','running','recovery','failed','succeeded','cancelled')),
 old_size INTEGER NOT NULL CHECK(old_size>0),work BLOB NOT NULL,error BLOB,result BLOB,commit_result BLOB,
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX writeback_job_queue ON writeback_jobs(state,updated_at,id);
CREATE INDEX writeback_source_fence ON writeback_jobs(owner,source_id,state);
CREATE TABLE writeback_operations (
 owner TEXT NOT NULL,key TEXT NOT NULL,request_digest TEXT NOT NULL,binding_digest TEXT NOT NULL,
 job_id TEXT NOT NULL REFERENCES writeback_jobs(id),expires_at TEXT NOT NULL,PRIMARY KEY(owner,key)
);
