CREATE TABLE media_derived_objects (
    id TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    publication_id TEXT NOT NULL UNIQUE REFERENCES media_publications(id),
    relationship_key TEXT NOT NULL,
    transform TEXT NOT NULL,
    parent_object_id TEXT,
    job_id TEXT,
    source_id TEXT,
    generation INTEGER NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('available','unavailable')),
    document BLOB NOT NULL,
    created_at TEXT NOT NULL
);
CREATE UNIQUE INDEX media_derived_available ON media_derived_objects(owner,relationship_key) WHERE state='available';
CREATE INDEX media_derived_job ON media_derived_objects(owner,job_id,source_id,generation);
