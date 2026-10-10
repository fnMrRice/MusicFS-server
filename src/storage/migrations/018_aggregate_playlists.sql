CREATE TABLE personal_playlists (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,rule_revision INTEGER NOT NULL,hide_revision INTEGER NOT NULL,
 snapshot_revision INTEGER NOT NULL,document BLOB NOT NULL
);
CREATE INDEX personal_playlists_owner ON personal_playlists(owner,id);
CREATE TABLE personal_playlist_snapshots (
 playlist_id TEXT NOT NULL REFERENCES personal_playlists(id),revision INTEGER NOT NULL,owner TEXT NOT NULL,
 document BLOB NOT NULL,PRIMARY KEY(playlist_id,revision)
);
CREATE TABLE playlist_hidden_rules (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,playlist_id TEXT NOT NULL REFERENCES personal_playlists(id),
 active INTEGER NOT NULL CHECK(active IN(0,1)),document BLOB NOT NULL
);
CREATE INDEX playlist_hidden_owner ON playlist_hidden_rules(owner,playlist_id,id);
CREATE TABLE playlist_changes (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,owner TEXT NOT NULL,playlist_id TEXT NOT NULL REFERENCES personal_playlists(id),
 document BLOB NOT NULL
);
CREATE INDEX playlist_changes_owner ON playlist_changes(owner,playlist_id,sequence);
