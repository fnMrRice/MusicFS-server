CREATE TABLE media_content_objects (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),
 asset_id TEXT NOT NULL REFERENCES source_assets(id),publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
 state TEXT NOT NULL CHECK(state IN ('available','unavailable')),document BLOB NOT NULL,created_at TEXT NOT NULL
);
CREATE INDEX media_content_owner ON media_content_objects(owner,source_id,id);
CREATE TABLE media_selection_revisions (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),revision INTEGER NOT NULL CHECK(revision>=0),
 PRIMARY KEY(owner,source_id)
);
CREATE TABLE media_content_bindings (
 id TEXT PRIMARY KEY,owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),
 object_id TEXT NOT NULL REFERENCES media_content_objects(id),asset_id TEXT NOT NULL REFERENCES source_assets(id),
 field TEXT NOT NULL,language TEXT NOT NULL,current INTEGER NOT NULL CHECK(current IN (0,1)),
 document BLOB NOT NULL,created_at TEXT NOT NULL
);
CREATE INDEX media_binding_history ON media_content_bindings(owner,source_id,created_at,id);
CREATE TABLE media_content_slots (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),field TEXT NOT NULL,language TEXT NOT NULL,
 binding_id TEXT NOT NULL REFERENCES media_content_bindings(id),manual_locked INTEGER NOT NULL CHECK(manual_locked IN (0,1)),
 PRIMARY KEY(owner,source_id,field,language)
);
CREATE TABLE media_selection_changes (
 owner TEXT NOT NULL,source_id TEXT NOT NULL REFERENCES tracks(id),revision INTEGER NOT NULL,
 operation TEXT NOT NULL,field TEXT NOT NULL,language TEXT NOT NULL,
 previous_binding_id TEXT,next_binding_id TEXT,manual_locked INTEGER NOT NULL,
 created_at TEXT NOT NULL,PRIMARY KEY(owner,source_id,revision,field,language)
);
