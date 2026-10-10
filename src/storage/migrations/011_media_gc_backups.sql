CREATE TABLE media_garbage (
 id TEXT PRIMARY KEY,publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
 object_id TEXT NOT NULL UNIQUE,state TEXT NOT NULL CHECK(state IN ('claimed','deleted')),
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE INDEX media_garbage_pending ON media_garbage(state,id);
CREATE TABLE media_backups (
 id TEXT PRIMARY KEY,database_path TEXT NOT NULL UNIQUE,
 state TEXT NOT NULL CHECK(state IN ('preparing','prepared','finished')),
 object_count INTEGER NOT NULL DEFAULT 0,missing_count INTEGER NOT NULL DEFAULT 0,
 created_at TEXT NOT NULL,updated_at TEXT NOT NULL
);
CREATE TABLE media_backup_objects (
 backup_id TEXT NOT NULL REFERENCES media_backups(id),object_id TEXT NOT NULL,
 owner TEXT NOT NULL,publication_id TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('pending','copied','missing')),document BLOB NOT NULL,
 PRIMARY KEY(backup_id,object_id)
);
CREATE INDEX media_backup_pins ON media_backup_objects(object_id);
