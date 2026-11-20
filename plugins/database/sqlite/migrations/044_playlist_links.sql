-- 即使不同 MusicFS 用户或实例登录同一平台账号，一个平台歌单也只对应一个本地歌单。
CREATE TABLE playlist_links (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner TEXT NOT NULL, platform TEXT NOT NULL, instance_id TEXT NOT NULL,
 account_id TEXT NOT NULL, external_id TEXT NOT NULL,
 playlist_id TEXT NOT NULL REFERENCES personal_playlists(id) ON DELETE CASCADE,
 UNIQUE(platform,external_id)
);
CREATE INDEX playlist_links_local ON playlist_links(owner,playlist_id);

-- 歌曲追加和待发送记录在同一事务内提交。解除关联会清除尚未发送的记录。
CREATE TABLE playlist_link_additions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 link_id INTEGER NOT NULL REFERENCES playlist_links(id) ON DELETE CASCADE,
 track_id TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, retry_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', UNIQUE(link_id,track_id)
);
CREATE INDEX playlist_link_additions_ready ON playlist_link_additions(retry_at,id);
