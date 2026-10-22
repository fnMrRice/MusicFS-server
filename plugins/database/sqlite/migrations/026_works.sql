CREATE TABLE works (
 id TEXT PRIMARY KEY, owner TEXT NOT NULL, content_type TEXT NOT NULL,
 title TEXT NOT NULL, description TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision>=1),
 document BLOB NOT NULL
);
CREATE INDEX works_owner ON works(owner,id);
CREATE INDEX works_type ON works(owner,content_type,id);
CREATE TABLE work_external_ids (
 work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 namespace TEXT NOT NULL, value TEXT NOT NULL, PRIMARY KEY(work_id,namespace,value)
);
CREATE INDEX work_external_value ON work_external_ids(namespace,value,work_id);
CREATE TABLE work_credits (
 work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 role TEXT NOT NULL, name TEXT NOT NULL, PRIMARY KEY(work_id,role,name)
);
CREATE INDEX work_credit_name ON work_credits(name,work_id);
CREATE TABLE work_tags (
 work_id TEXT NOT NULL REFERENCES works(id) ON DELETE CASCADE,
 tag TEXT NOT NULL, PRIMARY KEY(work_id,tag)
);
CREATE INDEX work_tag_name ON work_tags(tag,work_id);
