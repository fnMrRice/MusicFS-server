CREATE TABLE matching_revisions(owner TEXT PRIMARY KEY,revision INTEGER NOT NULL CHECK(revision>=0));
CREATE TABLE recordings (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX recordings_owner ON recordings(owner,id);
CREATE TABLE recording_source_links (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),recording_id TEXT REFERENCES recordings(id),
 decision TEXT NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(owner,source_id)
);
CREATE INDEX recording_links_members ON recording_source_links(owner,recording_id,source_id);
CREATE TABLE match_exclusions (
 owner TEXT NOT NULL,left_source TEXT NOT NULL REFERENCES tracks(id),right_source TEXT NOT NULL REFERENCES tracks(id),
 excluded INTEGER NOT NULL CHECK(excluded IN(0,1)),revision INTEGER NOT NULL,
 PRIMARY KEY(owner,left_source,right_source),CHECK(left_source<right_source)
);
CREATE TABLE link_changes (
 sequence INTEGER NOT NULL,owner TEXT NOT NULL,revision INTEGER NOT NULL,
 operation_id TEXT NOT NULL,document BLOB NOT NULL,PRIMARY KEY(owner,sequence)
);
CREATE INDEX link_changes_owner ON link_changes(owner,sequence);
CREATE TABLE matching_candidates (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,left_source TEXT NOT NULL,right_source TEXT NOT NULL,
 left_asset TEXT NOT NULL,right_asset TEXT NOT NULL,score INTEGER NOT NULL,status TEXT NOT NULL,
 binding_digest TEXT NOT NULL,document BLOB NOT NULL,created_at TEXT NOT NULL,CHECK(left_source<right_source)
);
CREATE INDEX matching_candidates_owner ON matching_candidates(owner,status,score DESC,id);
