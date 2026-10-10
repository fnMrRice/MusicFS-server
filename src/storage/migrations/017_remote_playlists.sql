CREATE TABLE remote_playlists (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,instance_id TEXT NOT NULL,account_id TEXT NOT NULL,external_id TEXT NOT NULL,
 binding_revision INTEGER NOT NULL,revision INTEGER NOT NULL,document BLOB NOT NULL,
 UNIQUE(owner,instance_id,account_id,external_id)
);
CREATE INDEX remote_playlists_owner ON remote_playlists(owner,id);
CREATE TABLE remote_sources (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,instance_id TEXT NOT NULL,account_id TEXT NOT NULL,external_id TEXT NOT NULL,
 document BLOB NOT NULL,UNIQUE(owner,instance_id,account_id,external_id)
);
CREATE TABLE remote_playlist_snapshots (
 playlist_id TEXT NOT NULL REFERENCES remote_playlists(id),revision INTEGER NOT NULL,
 owner TEXT NOT NULL,document BLOB NOT NULL,PRIMARY KEY(playlist_id,revision)
);
CREATE TABLE remote_sync_jobs (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,playlist_id TEXT NOT NULL REFERENCES remote_playlists(id),
 state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 generation INTEGER NOT NULL,binding_revision INTEGER NOT NULL,document BLOB NOT NULL,created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX remote_sync_active ON remote_sync_jobs(playlist_id) WHERE state IN ('queued','running');
CREATE INDEX remote_sync_queue ON remote_sync_jobs(state,created_at,id);
