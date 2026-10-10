CREATE TABLE source_assets (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, track_id TEXT NOT NULL,
 version TEXT NOT NULL, current INTEGER NOT NULL CHECK(current IN (0,1)),
 document BLOB NOT NULL, created_at TEXT NOT NULL, invalidated_at TEXT,
 file_sha512 BLOB CHECK(file_sha512 IS NULL OR length(file_sha512)=64)
);
CREATE INDEX source_assets_track ON source_assets(owner,track_id,current);
CREATE UNIQUE INDEX source_assets_current ON source_assets(owner,track_id) WHERE current=1;
CREATE TABLE asset_evidence (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, asset_id TEXT NOT NULL REFERENCES source_assets(id),
 kind TEXT NOT NULL, origin TEXT NOT NULL, generation INTEGER NOT NULL CHECK(generation>0),
 status TEXT NOT NULL, digest BLOB CHECK(digest IS NULL OR length(digest)=64),
 document BLOB NOT NULL, created_at TEXT NOT NULL, invalidated_at TEXT,
 UNIQUE(asset_id,kind,origin,generation)
);
CREATE INDEX asset_evidence_lookup ON asset_evidence(owner,asset_id,kind,origin,generation);
