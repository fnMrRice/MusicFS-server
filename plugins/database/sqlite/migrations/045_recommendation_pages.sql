-- 按用户、来源组合和登录代次保存整页推荐；递增编号区分同一时刻的多次刷新。
CREATE TABLE recommendation_pages (
    id INTEGER PRIMARY KEY,
    owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope TEXT NOT NULL,
    server_day TEXT NOT NULL,
    successful INTEGER NOT NULL CHECK (successful IN (0, 1)),
    sources_json TEXT NOT NULL
);
CREATE INDEX recommendation_page_history ON recommendation_pages(owner_id, scope, id DESC);
