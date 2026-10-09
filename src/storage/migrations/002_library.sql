CREATE TABLE library_revision (singleton INTEGER PRIMARY KEY CHECK(singleton=1),revision INTEGER NOT NULL);
INSERT INTO library_revision VALUES(1,0);
CREATE TABLE tracks (id TEXT PRIMARY KEY,owner TEXT NOT NULL,provider TEXT NOT NULL,external_id TEXT NOT NULL,root_id TEXT NOT NULL,path TEXT NOT NULL,version TEXT NOT NULL,asset_id TEXT NOT NULL,mime TEXT NOT NULL,size INTEGER NOT NULL,document BLOB NOT NULL,UNIQUE(owner,provider,external_id));
CREATE INDEX tracks_provider ON tracks(owner,provider);
CREATE TABLE library_snapshots (id TEXT PRIMARY KEY,owner TEXT NOT NULL,provider TEXT NOT NULL,revision TEXT NOT NULL,expires_at TEXT NOT NULL);
CREATE TABLE library_snapshot_items (snapshot_id TEXT NOT NULL REFERENCES library_snapshots(id) ON DELETE CASCADE,position INTEGER NOT NULL,document BLOB NOT NULL,PRIMARY KEY(snapshot_id,position));
CREATE TABLE library_cursors (id TEXT PRIMARY KEY,snapshot_id TEXT NOT NULL REFERENCES library_snapshots(id) ON DELETE CASCADE,position INTEGER NOT NULL);
