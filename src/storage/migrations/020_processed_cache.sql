CREATE TABLE processed_cache (
 id TEXT PRIMARY KEY, cache_key TEXT NOT NULL UNIQUE,
 owner TEXT NOT NULL, asset_id TEXT NOT NULL, asset_version TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK(size_bytes>0), sha256 TEXT NOT NULL,
 created_at TEXT NOT NULL, last_used_at TEXT NOT NULL, document BLOB NOT NULL
);
CREATE INDEX processed_cache_owner ON processed_cache(owner,last_used_at);
