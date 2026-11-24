-- 保存用户提交的全库操作及每次进度更新。请求按用户、任务种类和幂等键去重。
CREATE TABLE library_operations (
 id TEXT COLLATE "C" PRIMARY KEY,
 owner TEXT COLLATE "C" NOT NULL,
 kind TEXT COLLATE "C" NOT NULL CHECK(kind IN ('albumAutoScrape','bulkMerge')),
 request_key TEXT COLLATE "C" NOT NULL,
 payload TEXT COLLATE "C" NOT NULL,
 state TEXT COLLATE "C" NOT NULL CHECK(state IN ('queued','running','succeeded','failed','cancelled')),
 progress TEXT COLLATE "C" NOT NULL DEFAULT '{}',
 result TEXT COLLATE "C" NOT NULL DEFAULT '{}',
 failure TEXT COLLATE "C",
 created_at TEXT COLLATE "C" NOT NULL,
 updated_at TEXT COLLATE "C" NOT NULL,
 UNIQUE(owner,kind,request_key)
);
CREATE INDEX library_operations_ready ON library_operations(state,created_at,id);
CREATE INDEX library_operations_owner ON library_operations(owner,created_at,id);
