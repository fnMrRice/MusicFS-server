CREATE TABLE catalog_queries (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, query BLOB NOT NULL, revision TEXT NOT NULL,
 total INTEGER NOT NULL CHECK(total>=0), expires_at TEXT NOT NULL,
 cancelled INTEGER NOT NULL DEFAULT 0 CHECK(cancelled IN (0,1)), idempotency_key TEXT NOT NULL
);
CREATE INDEX catalog_query_owner ON catalog_queries(owner,expires_at);
CREATE UNIQUE INDEX catalog_query_idempotency ON catalog_queries(owner,idempotency_key) WHERE idempotency_key<>'';
CREATE TABLE catalog_query_items (
 query_id TEXT NOT NULL REFERENCES catalog_queries(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0), document BLOB NOT NULL, PRIMARY KEY(query_id,position)
);
CREATE TABLE catalog_query_cursors (
 id TEXT PRIMARY KEY, query_id TEXT NOT NULL REFERENCES catalog_queries(id) ON DELETE CASCADE,
 position INTEGER NOT NULL CHECK(position>=0), UNIQUE(query_id,position)
);
