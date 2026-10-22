-- 录音默认来源按所有者隔离，未保存偏好时按来源质量自动选择。
CREATE TABLE recording_default_sources (
 owner TEXT NOT NULL,
 recording_id TEXT NOT NULL REFERENCES recordings(id) ON DELETE CASCADE,
 source_id TEXT NOT NULL,
 PRIMARY KEY(owner,recording_id),
 FOREIGN KEY(owner,source_id) REFERENCES recording_source_links(owner,source_id) ON DELETE CASCADE
);

-- 解除、拆分或合并将来源移出原录音时，旧录音恢复自动选择。
CREATE TRIGGER recording_default_sources_member_moved
AFTER UPDATE OF recording_id ON recording_source_links
WHEN OLD.recording_id IS NOT NEW.recording_id
BEGIN
 DELETE FROM recording_default_sources WHERE owner=OLD.owner AND recording_id=OLD.recording_id AND source_id=OLD.source_id;
END;
