-- 每个来源可拥有多份独立下载。索引仅记录已经完整写入并发布的文件。
CREATE TABLE online_downloads (
 id TEXT PRIMARY KEY, lookup_key TEXT NOT NULL, owner TEXT NOT NULL, plugin_id TEXT NOT NULL,
 source_id TEXT NOT NULL REFERENCES tracks(id) ON DELETE CASCADE,
 document TEXT NOT NULL CHECK(json_valid(document))
);
CREATE INDEX online_downloads_source ON online_downloads(owner,source_id);
