CREATE TABLE timeline_evidence (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, recording_id TEXT NOT NULL,
 from_source TEXT NOT NULL, to_source TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>0), revoked_at TEXT,
 document BLOB NOT NULL, created_at TEXT NOT NULL
);
CREATE INDEX timeline_evidence_owner ON timeline_evidence(owner,from_source,to_source);
