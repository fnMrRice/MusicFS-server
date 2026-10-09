CREATE TABLE users (id TEXT PRIMARY KEY, role TEXT NOT NULL);
INSERT INTO users VALUES ('admin','admin');
CREATE TABLE auth_sessions (id TEXT PRIMARY KEY, user_id TEXT NOT NULL REFERENCES users(id), csrf TEXT NOT NULL, expires_at TEXT NOT NULL);
CREATE TABLE jobs (id TEXT PRIMARY KEY, owner TEXT NOT NULL, target TEXT NOT NULL, kind TEXT NOT NULL, generation INTEGER NOT NULL, state TEXT NOT NULL, observed INTEGER NOT NULL DEFAULT 0, complete INTEGER NOT NULL DEFAULT 0, error TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_success_at TEXT);
CREATE INDEX jobs_target ON jobs(owner,target,state);
CREATE TABLE idempotency (owner TEXT NOT NULL, route TEXT NOT NULL, key TEXT NOT NULL, digest TEXT NOT NULL, result BLOB NOT NULL, expires_at TEXT NOT NULL, PRIMARY KEY(owner,route,key));
