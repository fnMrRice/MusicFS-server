-- 保存用户提交的全库操作及每次进度更新。请求按用户、任务种类和幂等键去重。
CREATE TABLE library_operations (
 id TEXT PRIMARY KEY,
 owner TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('albumAutoScrape','bulkMerge')),
 request_key TEXT NOT NULL,
 payload TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 progress TEXT NOT NULL DEFAULT '{}',
 result TEXT NOT NULL DEFAULT '{}',
 failure TEXT,
 created_at TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 UNIQUE(owner,kind,request_key)
);
CREATE INDEX library_operations_ready ON library_operations(state,created_at,id);
CREATE INDEX library_operations_owner ON library_operations(owner,created_at,id);
