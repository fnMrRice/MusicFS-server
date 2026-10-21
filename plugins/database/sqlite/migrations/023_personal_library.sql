CREATE TABLE personal_profiles (
 owner TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,nickname TEXT
);
CREATE TABLE personal_favorites (
 owner TEXT NOT NULL,source_track_id TEXT NOT NULL,created_at TEXT NOT NULL,
 PRIMARY KEY(owner,source_track_id)
);
CREATE INDEX personal_favorites_order ON personal_favorites(owner,created_at DESC,source_track_id);
CREATE TABLE personal_recently_played (
 owner TEXT NOT NULL,source_track_id TEXT NOT NULL,played_at TEXT NOT NULL,
 PRIMARY KEY(owner,source_track_id)
);
CREATE INDEX personal_recently_played_order ON personal_recently_played(owner,played_at DESC,source_track_id);
CREATE TABLE personal_playback_receipts (
 owner TEXT NOT NULL,playback_session_id TEXT NOT NULL,source_track_id TEXT NOT NULL,
 recorded_at TEXT NOT NULL,expires_at TEXT NOT NULL,
 PRIMARY KEY(owner,playback_session_id)
);
CREATE TABLE personal_collection_revisions (
 owner TEXT NOT NULL,kind TEXT NOT NULL,revision INTEGER NOT NULL,
 PRIMARY KEY(owner,kind)
);
CREATE TABLE personal_collection_cursors (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,kind TEXT NOT NULL,revision TEXT NOT NULL,
 position INTEGER NOT NULL,expires_at TEXT NOT NULL
);
CREATE INDEX personal_collection_cursors_owner ON personal_collection_cursors(owner,kind);
