-- 自建歌单保留删除级联；空外键表示该用户独立的系统收藏。
ALTER TABLE playlist_links RENAME TO previous_playlist_links;
ALTER TABLE playlist_link_additions RENAME TO previous_playlist_link_additions;

CREATE TABLE playlist_links (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 owner TEXT NOT NULL, platform TEXT NOT NULL, instance_id TEXT NOT NULL,
 account_id TEXT NOT NULL, external_id TEXT NOT NULL,
 personal_playlist_id TEXT REFERENCES personal_playlists(id) ON DELETE CASCADE,
 playlist_id TEXT GENERATED ALWAYS AS (coalesce(personal_playlist_id,'favorites')) VIRTUAL,
 UNIQUE(platform,external_id)
);
CREATE TABLE playlist_link_additions (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 link_id INTEGER NOT NULL REFERENCES playlist_links(id) ON DELETE CASCADE,
 track_id TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, retry_at INTEGER NOT NULL DEFAULT 0,
 last_error TEXT NOT NULL DEFAULT '', UNIQUE(link_id,track_id)
);

INSERT INTO playlist_links(id,owner,platform,instance_id,account_id,external_id,personal_playlist_id)
 SELECT id,owner,platform,instance_id,account_id,external_id,playlist_id FROM previous_playlist_links;
INSERT INTO playlist_link_additions(id,link_id,track_id,attempts,retry_at,last_error)
 SELECT id,link_id,track_id,attempts,retry_at,last_error FROM previous_playlist_link_additions;

-- 已完成的投递可能已删除，保留两个表的自增序列，避免复用旧编号。
UPDATE sqlite_sequence AS current SET seq=max(seq,coalesce((
 SELECT previous.seq FROM sqlite_sequence previous WHERE previous.name='previous_'||current.name),0))
 WHERE name IN ('playlist_links','playlist_link_additions');
DROP TABLE previous_playlist_link_additions;
DROP TABLE previous_playlist_links;
CREATE INDEX playlist_links_local ON playlist_links(owner,playlist_id);
CREATE INDEX playlist_link_additions_ready ON playlist_link_additions(retry_at,id);
