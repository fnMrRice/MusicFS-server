-- 人工封面偏好独立修订；保留失效对象编号，不为媒体垃圾回收新增引用。
CREATE TABLE recording_cover_preferences (
 owner TEXT NOT NULL,
 recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
 revision INTEGER NOT NULL CHECK(revision>0),
 object_id TEXT,
 PRIMARY KEY(owner,recording_id)
);
