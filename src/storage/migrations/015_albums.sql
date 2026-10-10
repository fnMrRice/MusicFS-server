CREATE TABLE source_albums (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,provider_id TEXT NOT NULL,name TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX source_albums_owner ON source_albums(owner,provider_id,id);
CREATE TABLE source_album_members (
 album_id TEXT NOT NULL REFERENCES source_albums(id),source_id TEXT NOT NULL REFERENCES tracks(id),
 position INTEGER NOT NULL CHECK(position>=0),PRIMARY KEY(album_id,source_id),UNIQUE(album_id,position)
);
CREATE TABLE releases (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,name TEXT NOT NULL,revision INTEGER NOT NULL CHECK(revision>=1),
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX releases_owner ON releases(owner,id);
CREATE TABLE release_albums (
 release_id TEXT NOT NULL REFERENCES releases(id),album_id TEXT NOT NULL REFERENCES source_albums(id),
 position INTEGER NOT NULL CHECK(position>=0),PRIMARY KEY(release_id,album_id),UNIQUE(release_id,position)
);
CREATE TABLE album_cover_bindings (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,target_kind TEXT NOT NULL CHECK(target_kind IN ('sourceAlbum','release')),
 target_id TEXT NOT NULL,object_id TEXT NOT NULL REFERENCES media_content_objects(id),
 target_revision INTEGER NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),asset_id TEXT NOT NULL REFERENCES source_assets(id),
 asset_version TEXT NOT NULL,created_at TEXT NOT NULL
);
CREATE INDEX album_cover_history ON album_cover_bindings(owner,target_kind,target_id,id);
CREATE TABLE album_cover_heads (
 owner TEXT NOT NULL,target_kind TEXT NOT NULL CHECK(target_kind IN ('sourceAlbum','release')),target_id TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1),binding_id TEXT NOT NULL REFERENCES album_cover_bindings(id),
 manual_locked INTEGER NOT NULL CHECK(manual_locked IN (0,1)),PRIMARY KEY(owner,target_kind,target_id)
);
CREATE TABLE album_changes (
 sequence INTEGER PRIMARY KEY AUTOINCREMENT,owner TEXT NOT NULL,target_kind TEXT NOT NULL,target_id TEXT NOT NULL,
 document BLOB NOT NULL
);
CREATE INDEX album_changes_target ON album_changes(owner,target_kind,target_id,sequence);
