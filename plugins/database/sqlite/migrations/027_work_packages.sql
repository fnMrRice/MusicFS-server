CREATE TABLE work_packages (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL, root_id TEXT NOT NULL, relative_path TEXT NOT NULL,
 revision INTEGER NOT NULL CHECK(revision>=1), document BLOB NOT NULL,
 UNIQUE(owner,provider_id,root_id,relative_path)
);
CREATE INDEX work_packages_work ON work_packages(owner,work_id,id);
