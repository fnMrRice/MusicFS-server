-- 原生发行号由宿主从原始观测核实。实例间仅按同一账号、同一目录、同一发行号归一。
CREATE TABLE music_album_release_identities (
 owner TEXT NOT NULL, namespace TEXT NOT NULL, release_id TEXT NOT NULL, album_id TEXT NOT NULL,
 PRIMARY KEY(owner,namespace,release_id), UNIQUE(owner,album_id),
 FOREIGN KEY(owner,album_id) REFERENCES music_albums(owner,id) ON DELETE CASCADE DEFERRABLE INITIALLY DEFERRED,
 CHECK((namespace='netease' AND length(release_id) BETWEEN 1 AND 20 AND substr(release_id,1,1) BETWEEN '1' AND '9' AND release_id NOT GLOB '*[^0-9]*')
    OR (namespace='qqmusic' AND length(release_id) BETWEEN 1 AND 64 AND release_id NOT GLOB '*[^A-Za-z0-9]*'))
);

-- 记住上一次真实的标签成员；同标签无编号重扫可以延续，标签改变后重新解析。
CREATE TABLE music_album_source_tag_history (
 owner TEXT NOT NULL, source_id TEXT NOT NULL, tag_name TEXT NOT NULL, album_id TEXT NOT NULL,
 PRIMARY KEY(owner,source_id), FOREIGN KEY(owner,source_id) REFERENCES tracks(owner,id) ON DELETE CASCADE,
 FOREIGN KEY(owner,album_id) REFERENCES music_albums(owner,id) ON DELETE CASCADE
);
CREATE INDEX music_album_source_history_album ON music_album_source_tag_history(owner,album_id);
INSERT INTO music_album_source_tag_history(owner,source_id,tag_name,album_id)
 SELECT o.owner,o.id,o.album,min(r.album_id) FROM music_observed_tracks o JOIN music_album_source_refs r ON r.source_id=o.id
 WHERE r.origin_id='tag' AND o.album IS NOT NULL AND NOT EXISTS(SELECT 1 FROM music_album_source_tag_overrides s
  WHERE s.owner=o.owner AND s.source_id=o.id AND s.tag_name=o.album) GROUP BY o.owner,o.id,o.album HAVING count(*)=1;
ALTER TABLE music_albums ADD COLUMN unresolved_tag TEXT;
ALTER TABLE music_albums ADD COLUMN native_displaced INTEGER NOT NULL DEFAULT 0 CHECK(native_displaced IN (0,1));
-- 0 表示既有卡，1 表示扫描临时创建，2 表示临时卡已被编辑或选用。
ALTER TABLE music_albums ADD COLUMN native_generated INTEGER NOT NULL DEFAULT 0 CHECK(native_generated IN (0,1,2));
CREATE UNIQUE INDEX music_album_unresolved_tag ON music_albums(owner,tag_provider,unresolved_tag);

CREATE VIEW music_observed_album_releases AS
 SELECT id,owner,namespace,release_id FROM (
  SELECT id,owner,json_extract(data,'$.albumRelease.namespace') AS namespace,json_extract(data,'$.albumRelease.id') AS release_id
  FROM (SELECT id,owner,CASE WHEN json_valid(document) THEN document END AS data FROM tracks) t
  WHERE json_type(data,'$.albumRelease.namespace')='text' AND json_type(data,'$.albumRelease.id')='text'
 ) raw WHERE (namespace='netease' AND length(release_id) BETWEEN 1 AND 20 AND substr(release_id,1,1) BETWEEN '1' AND '9' AND release_id NOT GLOB '*[^0-9]*')
    OR (namespace='qqmusic' AND length(release_id) BETWEEN 1 AND 64 AND release_id NOT GLOB '*[^A-Za-z0-9]*');

-- 当前音源的逐源覆盖、实例原标签以及独立的原生声明。已有归属从不依赖歌曲标题比较。
CREATE VIEW music_source_album_choices AS
 SELECT o.id,o.owner,o.provider,o.album,n.namespace,n.release_id,s.album_id AS override_id,
  coalesce(k.album_id,a.id) AS legacy_id,h.album_id AS previous_id
 FROM music_observed_tracks o
 LEFT JOIN music_observed_album_releases n ON n.id=o.id
 LEFT JOIN music_album_source_tag_overrides s ON s.owner=o.owner AND s.source_id=o.id AND s.tag_name=o.album
 LEFT JOIN music_album_tag_aliases k ON k.owner=o.owner AND k.provider=o.provider AND k.tag_name=o.album
 LEFT JOIN music_albums a ON a.owner=o.owner AND a.tag_provider=o.provider AND a.tag_name=o.album
 LEFT JOIN music_album_source_tag_history h ON h.owner=o.owner AND h.source_id=o.id AND h.tag_name=o.album
 WHERE o.album IS NOT NULL;

CREATE VIEW music_album_selected_releases AS
 SELECT f.album_id,f.field,CASE json_extract(f.document,'$.pluginId')
   WHEN 'musicfs.scraper.netease' THEN 'netease' WHEN 'musicfs.scraper.qqmusic' THEN 'qqmusic' END AS namespace,
  json_extract(f.document,'$.item.id') AS release_id
 FROM music_album_scraped_fields f WHERE f.field IN ('albumDetails','albumTracks');

-- 链接只识别官方专辑路径。路径之外的数字、短链和歌曲链接均不能声明发行。
CREATE VIEW music_album_native_links AS
 SELECT a.id AS album_id,json_extract(j.value,'$.pluginId') AS plugin_id,
  json_extract(j.value,'$.url') AS url
 FROM music_albums a,json_each(a.source_links) j WHERE j.type='object';

CREATE VIEW music_album_native_edition_links AS
 SELECT album_id,CASE plugin_id WHEN 'musicfs.scraper.netease' THEN 'netease' ELSE 'qqmusic' END AS namespace,
 CASE
  WHEN plugin_id='musicfs.scraper.netease' AND substr(url,1,31)='https://music.163.com/album?id=' THEN substr(url,32)
  WHEN plugin_id='musicfs.scraper.netease' AND substr(url,1,33)='https://music.163.com/#/album?id=' THEN substr(url,34)
  WHEN plugin_id='musicfs.scraper.netease' AND substr(url,1,33)='https://music.163.com/m/album?id=' THEN substr(url,34)
  WHEN plugin_id='musicfs.scraper.qqmusic' AND substr(url,1,36)='https://y.qq.com/n/ryqq/albumDetail/' THEN substr(url,37)
  WHEN plugin_id='musicfs.scraper.qqmusic' AND substr(url,1,39)='https://y.qq.com/n/ryqq_v2/albumDetail/' THEN substr(url,40)
 END AS release_id
 FROM music_album_native_links WHERE plugin_id IN ('musicfs.scraper.netease','musicfs.scraper.qqmusic');

-- 每张卡只索引原生目录与发行号。完整曲表和链接 JSON 只在该字段更新时解析一次。
CREATE TABLE music_album_release_hints (
 owner TEXT NOT NULL,album_id TEXT NOT NULL,kind TEXT NOT NULL,
 namespace TEXT NOT NULL,release_id TEXT NOT NULL,
 PRIMARY KEY(owner,album_id,namespace,release_id,kind),
 FOREIGN KEY(owner,album_id) REFERENCES music_albums(owner,id) ON DELETE CASCADE
);
CREATE INDEX music_album_release_hint_album ON music_album_release_hints(album_id,kind);
CREATE INDEX music_album_release_hint_lookup ON music_album_release_hints(owner,namespace,release_id,album_id);
CREATE VIEW music_album_hint_entries AS
 SELECT album_id,'link' AS kind,namespace,coalesce(release_id,'') AS release_id FROM music_album_native_edition_links
 UNION ALL SELECT album_id,field,namespace,coalesce(release_id,'') FROM music_album_selected_releases WHERE namespace IS NOT NULL;
INSERT INTO music_album_release_hints(owner,album_id,kind,namespace,release_id)
 SELECT a.owner,h.album_id,h.kind,h.namespace,h.release_id FROM music_album_hint_entries h JOIN music_albums a ON a.id=h.album_id
 WHERE 1=1 ON CONFLICT DO NOTHING;
CREATE VIEW music_refresh_album_hints AS SELECT album_id,kind FROM music_album_release_hints;
CREATE TRIGGER music_refresh_album_hint_field INSTEAD OF INSERT ON music_refresh_album_hints BEGIN
 DELETE FROM music_album_release_hints WHERE album_id=NEW.album_id AND kind=NEW.kind;
 INSERT INTO music_album_release_hints(owner,album_id,kind,namespace,release_id)
 SELECT a.owner,h.album_id,h.kind,h.namespace,h.release_id FROM music_album_hint_entries h JOIN music_albums a ON a.id=h.album_id
 WHERE h.album_id=NEW.album_id AND h.kind=NEW.kind ON CONFLICT DO NOTHING;
END;
CREATE TRIGGER music_album_link_hints_insert AFTER INSERT ON music_albums WHEN NEW.source_links<>'[]' BEGIN
 INSERT INTO music_refresh_album_hints VALUES(NEW.id,'link');
END;
CREATE TRIGGER music_album_link_hints_update AFTER UPDATE OF source_links ON music_albums WHEN OLD.source_links<>NEW.source_links BEGIN
 INSERT INTO music_refresh_album_hints VALUES(NEW.id,'link');
END;
CREATE TRIGGER music_album_field_hints_insert AFTER INSERT ON music_album_scraped_fields WHEN NEW.field IN ('albumDetails','albumTracks') BEGIN
 INSERT INTO music_refresh_album_hints VALUES(NEW.album_id,NEW.field);
END;
CREATE TRIGGER music_album_field_hints_update AFTER UPDATE ON music_album_scraped_fields BEGIN
 DELETE FROM music_album_release_hints WHERE album_id=OLD.album_id AND kind=OLD.field;
 INSERT INTO music_refresh_album_hints SELECT NEW.album_id,NEW.field WHERE NEW.field IN ('albumDetails','albumTracks');
END;
CREATE TRIGGER music_album_field_hints_delete AFTER DELETE ON music_album_scraped_fields BEGIN
 DELETE FROM music_album_release_hints WHERE album_id=OLD.album_id AND kind=OLD.field;
END;

-- 新实例先到时，只反查已经显式归并、仍有真实成员的唯一旧卡。按目录号反查，不按专辑名查找。
CREATE VIEW music_unforced_album_releases AS
 SELECT n.id,n.namespace,n.release_id FROM music_observed_album_releases n JOIN music_observed_tracks o ON o.id=n.id
 WHERE o.album IS NOT NULL AND NOT EXISTS(SELECT 1 FROM music_album_source_tag_overrides s
  WHERE s.owner=o.owner AND s.source_id=o.id AND s.tag_name=o.album);

-- 所有真实旧成员都有同版观测，或有同平台发行级佐证，且没有任何反证。
-- 逐源例外不能为其他音源认领发行。跨目录快照需要同平台的明确专辑链接佐证。
CREATE VIEW music_album_release_claims AS
 SELECT c.id AS source_id,a.id AS album_id,CASE WHEN c.legacy_id=a.id OR c.previous_id=a.id THEN 0 ELSE 1 END AS external_lookup
 FROM music_source_album_choices c JOIN music_albums a ON a.owner=c.owner AND a.origin<>'cd' AND a.id IN (
  SELECT c.legacy_id
  UNION ALL SELECT c.previous_id WHERE EXISTS(SELECT 1 FROM music_albums previous WHERE previous.id=c.previous_id AND
   (previous.metadata_locked=1 OR EXISTS(SELECT 1 FROM music_album_tag_aliases m WHERE m.owner=c.owner AND m.album_id=previous.id)))
  UNION ALL SELECT k.album_id FROM music_album_release_hints k WHERE k.owner=c.owner AND k.namespace=c.namespace
   AND k.release_id=c.release_id AND k.kind IN ('link','albumDetails')
   AND EXISTS(SELECT 1 FROM music_album_tag_aliases m WHERE m.owner=c.owner AND m.album_id=k.album_id)
   AND EXISTS(SELECT 1 FROM music_album_source_refs r WHERE r.album_id=k.album_id AND r.origin_id='tag'))
 WHERE c.namespace IS NOT NULL AND c.override_id IS NULL
 AND NOT EXISTS(SELECT 1 FROM music_album_release_hints k WHERE k.owner=c.owner AND k.namespace=c.namespace
  AND k.release_id=c.release_id AND k.album_id<>a.id AND NOT EXISTS(
   SELECT 1 FROM music_album_release_identities bound JOIN music_albums generated ON generated.id=bound.album_id
   WHERE c.previous_id=a.id AND bound.owner=c.owner AND bound.namespace=c.namespace AND bound.release_id=c.release_id
   AND bound.album_id=k.album_id AND generated.native_generated>0))
 AND NOT EXISTS(SELECT 1 FROM music_album_release_identities i WHERE i.owner=c.owner AND i.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_release_hints f WHERE f.owner=c.owner AND f.album_id=a.id
  AND f.namespace=c.namespace AND f.release_id<>c.release_id)
 AND NOT EXISTS(SELECT 1 FROM music_album_source_refs r JOIN music_unforced_album_releases other ON other.id=r.source_id
  WHERE r.album_id=a.id AND r.origin_id='tag'
  AND (other.namespace<>c.namespace OR other.release_id<>c.release_id))
 AND ((EXISTS(SELECT 1 FROM music_album_release_hints f WHERE f.owner=c.owner AND f.album_id=a.id AND f.kind='albumDetails'
       AND f.namespace=c.namespace AND f.release_id=c.release_id)
   AND (c.previous_id=a.id OR EXISTS(SELECT 1 FROM music_album_tag_aliases k WHERE k.owner=c.owner AND k.album_id=a.id)
    OR EXISTS(SELECT 1 FROM music_album_source_refs r JOIN music_unforced_album_releases other ON other.id=r.source_id
     WHERE r.album_id=a.id AND r.origin_id='tag' AND other.namespace=c.namespace AND other.release_id=c.release_id)))
  OR (EXISTS(SELECT 1 FROM music_album_release_hints l WHERE l.owner=c.owner AND l.album_id=a.id AND l.kind='link' AND l.namespace=c.namespace AND l.release_id=c.release_id)
   AND (c.previous_id=a.id OR EXISTS(SELECT 1 FROM music_album_tag_aliases k WHERE k.owner=c.owner AND k.album_id=a.id))
   AND (c.previous_id=a.id OR NOT EXISTS(SELECT 1 FROM music_album_release_hints l WHERE l.owner=c.owner AND l.album_id=a.id AND l.namespace<>c.namespace)
    OR EXISTS(SELECT 1 FROM music_album_source_refs r JOIN music_unforced_album_releases other ON other.id=r.source_id
     WHERE r.album_id=a.id AND r.origin_id='tag'
     AND other.namespace=c.namespace AND other.release_id=c.release_id)))
  -- 跨目录的手工曲表可延续已有成员的身份：音源确实占有选用的曲位，每一条非人工例外的真实来源都有同版证据。
  OR (c.previous_id=a.id AND (a.metadata_locked=1 OR EXISTS(SELECT 1 FROM music_album_tag_aliases m WHERE m.owner=c.owner AND m.album_id=a.id))
   AND EXISTS(SELECT 1 FROM music_sources s JOIN music_album_scraped_track_refs slot ON slot.track_id=s.track_id AND slot.album_id=a.id
    WHERE s.id=c.id AND s.owner=c.owner)
   AND NOT EXISTS(SELECT 1 FROM music_album_source_refs r JOIN music_observed_tracks o ON o.id=r.source_id
    WHERE r.album_id=a.id AND r.origin_id='tag'
    AND NOT EXISTS(SELECT 1 FROM music_album_source_tag_overrides x WHERE x.owner=o.owner AND x.source_id=o.id AND x.tag_name=o.album)
    AND NOT EXISTS(SELECT 1 FROM music_observed_album_releases n WHERE n.id=o.id AND n.namespace=c.namespace AND n.release_id=c.release_id)))
  OR (c.previous_id=a.id AND NOT EXISTS(SELECT 1 FROM music_album_selected_releases f WHERE f.album_id=a.id)
   AND NOT EXISTS(SELECT 1 FROM music_album_source_refs r WHERE r.album_id=a.id AND r.origin_id='tag'
    AND NOT EXISTS(SELECT 1 FROM music_unforced_album_releases other WHERE other.id=r.source_id))));

-- 已定版专辑的同名新音源暂存为本实例的未识别专辑，防止纯标签把新歌直接归入已知发行。
CREATE VIEW music_unidentified_album_sources AS
 SELECT c.id,c.owner,c.provider,c.album FROM music_source_album_choices c
 WHERE c.override_id IS NULL AND c.namespace IS NULL AND c.previous_id IS NULL AND c.legacy_id IS NOT NULL
 AND (EXISTS(SELECT 1 FROM music_album_release_identities i WHERE i.owner=c.owner AND i.album_id=c.legacy_id)
  OR EXISTS(SELECT 1 FROM music_album_selected_releases f WHERE f.album_id=c.legacy_id)
  OR EXISTS(SELECT 1 FROM music_album_release_hints l WHERE l.owner=c.owner AND l.album_id=c.legacy_id AND l.kind='link' AND l.release_id<>''));

-- 已确认的历史成员回到原卡。另一张已人工干预的卡保持其身份和成员，仅保留原成员的例外。
CREATE VIEW music_proven_legacy_members AS
 SELECT c.id,c.owner,c.namespace,c.release_id,c.previous_id AS retained_id,i.album_id AS generated_id
 FROM music_source_album_choices c JOIN music_album_release_identities i ON i.owner=c.owner AND i.namespace=c.namespace AND i.release_id=c.release_id
 JOIN music_albums old ON old.id=c.previous_id AND old.owner=c.owner AND old.native_generated=0
 JOIN music_albums transient ON transient.id=i.album_id AND transient.native_generated>0
 WHERE c.override_id IS NULL AND c.previous_id<>i.album_id
 AND (old.metadata_locked=1 OR EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=old.id)
  OR EXISTS(SELECT 1 FROM music_album_tag_aliases k WHERE k.owner=c.owner AND k.album_id=old.id)
  OR EXISTS(SELECT 1 FROM music_album_source_tag_overrides x WHERE x.owner=c.owner AND x.album_id=old.id)
  OR EXISTS(SELECT 1 FROM music_manual_album_tracks m WHERE m.album_id=old.id))
 AND EXISTS(SELECT 1 FROM music_album_source_refs r WHERE r.source_id=c.id AND r.album_id=old.id AND r.origin_id='tag')
 AND EXISTS(SELECT 1 FROM music_album_release_claims h WHERE h.source_id=c.id AND h.album_id=old.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_release_claims h WHERE h.source_id=c.id AND h.album_id<>old.id);

-- 只有扫描器自己创建、从未被选用、仍然只有同版自动来源的临时卡可以退役。
CREATE VIEW music_album_release_recoveries AS
 SELECT p.* FROM music_proven_legacy_members p JOIN music_albums a ON a.id=p.generated_id
 WHERE a.native_generated=1 AND a.origin='tag' AND a.tag_provider IS NULL AND a.tag_name IS NULL AND a.unresolved_tag IS NULL
 AND a.metadata_locked=0 AND a.name=a.original_name AND a.source_links='[]' AND a.is_external=0
 AND a.external_id IS NULL AND a.cover_path IS NULL AND a.publisher IS NULL AND a.release_date IS NULL
 AND NOT EXISTS(SELECT 1 FROM idempotency receipt WHERE receipt.owner=p.owner AND receipt.route IN
  ('music/albums/'||a.id,'music/albums/scraped-clear/'||a.id,'music/albums/merge-tracks/'||a.id,'music/albums/metadata-merge/'||a.id))
 AND NOT EXISTS(SELECT 1 FROM music_manual_album_tracks m WHERE m.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_scraped_track_refs f WHERE f.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_merged_slots m WHERE m.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_tag_aliases k WHERE k.owner=p.owner AND k.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_source_tag_overrides x WHERE x.owner=p.owner AND x.album_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM music_album_source_tag_history h WHERE h.owner=p.owner AND h.album_id=a.id AND NOT EXISTS(
  SELECT 1 FROM music_album_source_refs r WHERE r.album_id=a.id AND r.source_id=h.source_id AND r.origin_id='tag'))
 AND NOT EXISTS(SELECT 1 FROM music_album_source_refs r WHERE r.album_id=a.id AND (r.origin_id<>'tag' OR NOT EXISTS(
  SELECT 1 FROM music_source_album_choices other WHERE other.id=r.source_id AND other.previous_id=a.id AND other.override_id IS NULL
  AND (other.namespace IS NULL OR (other.namespace=p.namespace AND other.release_id=p.release_id)))));

CREATE TRIGGER music_protect_generated_0 AFTER INSERT ON music_manual_album_tracks BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_generated_1 AFTER INSERT ON music_album_scraped_fields BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_generated_2 AFTER INSERT ON music_album_source_tag_overrides BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_generated_3 AFTER INSERT ON music_album_tag_aliases BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_reassigned_0 AFTER UPDATE OF album_id ON music_manual_album_tracks WHEN NEW.album_id<>OLD.album_id BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_reassigned_1 AFTER UPDATE OF album_id ON music_album_scraped_fields WHEN NEW.album_id<>OLD.album_id BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_reassigned_2 AFTER UPDATE OF album_id ON music_album_source_tag_overrides WHEN NEW.album_id<>OLD.album_id BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_protect_reassigned_3 AFTER UPDATE OF album_id ON music_album_tag_aliases WHEN NEW.album_id<>OLD.album_id BEGIN
 UPDATE music_albums SET native_generated=2 WHERE id=NEW.album_id AND native_generated=1; END;
CREATE TRIGGER music_recover_album_release INSTEAD OF INSERT ON music_album_release_recoveries BEGIN
 UPDATE music_tracks SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE owner=NEW.owner AND id IN (SELECT s.track_id FROM music_album_source_refs r JOIN music_sources s ON s.id=r.source_id WHERE r.album_id=NEW.generated_id);
 UPDATE music_albums SET revision=revision+1 WHERE id=NEW.retained_id;
 INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
 SELECT NEW.retained_id,r.source_id,r.origin_id,r.position FROM music_album_source_refs r WHERE r.album_id=NEW.generated_id
 ON CONFLICT(album_id,origin_id,source_id) DO NOTHING;
 UPDATE music_album_source_tag_history SET album_id=NEW.retained_id WHERE owner=NEW.owner AND album_id=NEW.generated_id;
 UPDATE music_album_release_identities SET album_id=NEW.retained_id WHERE owner=NEW.owner AND namespace=NEW.namespace
  AND release_id=NEW.release_id AND album_id=NEW.generated_id;
 DELETE FROM music_albums WHERE owner=NEW.owner AND id=NEW.generated_id;
END;

-- 最终标签归属同时用于成员写入与有效专辑集合的修订判断。
CREATE VIEW music_album_source_destinations AS
 SELECT coalesce(c.override_id,CASE WHEN c.previous_id<>i.album_id THEN
  (SELECT p.retained_id FROM music_proven_legacy_members p WHERE p.id=c.id) END,i.album_id,c.previous_id,
  (SELECT u.id FROM music_unidentified_album_sources p JOIN music_albums u
   ON u.owner=p.owner AND u.tag_provider=p.provider AND u.unresolved_tag=p.album WHERE p.id=c.id),c.legacy_id) AS album_id,c.id
 FROM music_source_album_choices c
 LEFT JOIN music_album_release_identities i ON i.owner=c.owner AND i.namespace=c.namespace AND i.release_id=c.release_id;

-- 只记录已有音源的逻辑曲目成员集合发生改变的专辑。手工归属、同录音另一来源仍在时不计移除。
CREATE VIEW music_album_membership_changes AS
 SELECT r.source_id,r.album_id,s.track_id FROM music_album_source_refs r JOIN music_sources s ON s.id=r.source_id
 WHERE r.origin_id='tag' AND NOT EXISTS(SELECT 1 FROM music_album_source_destinations d WHERE d.id=r.source_id AND d.album_id=r.album_id)
 AND NOT EXISTS(SELECT 1 FROM music_manual_album_tracks m WHERE m.album_id=r.album_id AND m.track_id=s.track_id)
 AND NOT EXISTS(SELECT 1 FROM music_album_source_refs other JOIN music_sources os ON os.id=other.source_id
  WHERE other.album_id=r.album_id AND os.track_id=s.track_id AND (other.source_id<>r.source_id OR other.origin_id<>'tag'))
 UNION ALL
 SELECT d.id,d.album_id,s.track_id FROM music_album_source_destinations d JOIN music_sources s ON s.id=d.id
 WHERE EXISTS(SELECT 1 FROM music_album_source_refs old_ref WHERE old_ref.source_id=d.id AND old_ref.origin_id='tag' AND old_ref.album_id<>d.album_id)
 AND NOT EXISTS(SELECT 1 FROM music_album_tracks m WHERE m.album_id=d.album_id AND m.track_id=s.track_id);

DROP TRIGGER IF EXISTS music_import_source;
CREATE TRIGGER music_import_source INSTEAD OF INSERT ON music_import_sources BEGIN
 INSERT INTO music_tracks(id,owner,title,genre,year,origin,metadata_source_id,created_at,updated_at)
 SELECT o.track_id,o.owner,o.title,o.genre,o.year,CASE WHEN o.track_id=o.id THEN 'source' ELSE 'recording' END,o.id,
  coalesce(r.created_at,(SELECT min(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),
           strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  coalesce(r.updated_at,(SELECT max(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),
           strftime('%Y-%m-%dT%H:%M:%fZ','now'))
 FROM music_observed_tracks o LEFT JOIN recordings r ON r.id=o.track_id AND r.owner=o.owner WHERE o.id=NEW.id
 ON CONFLICT(id) DO NOTHING;
 INSERT INTO music_sources(id,owner,track_id,plugin_id,provider_instance_id,external_id,created_at,updated_at)
 SELECT o.id,o.owner,o.track_id,p.plugin_id,o.provider,o.external_id,
  coalesce((SELECT min(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  coalesce((SELECT max(a.created_at) FROM source_assets a WHERE a.owner=o.owner AND a.track_id=o.id),strftime('%Y-%m-%dT%H:%M:%fZ','now'))
 FROM music_observed_tracks o LEFT JOIN music_source_providers p ON p.provider_instance_id=o.provider WHERE o.id=NEW.id
 ON CONFLICT(id) DO UPDATE SET track_id=excluded.track_id,plugin_id=excluded.plugin_id,
  provider_instance_id=excluded.provider_instance_id,external_id=excluded.external_id,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE music_sources.track_id<>excluded.track_id OR music_sources.plugin_id IS NOT excluded.plugin_id
  OR music_sources.provider_instance_id<>excluded.provider_instance_id OR music_sources.external_id<>excluded.external_id;


 INSERT INTO music_album_release_recoveries
 SELECT p.* FROM music_album_release_recoveries p WHERE p.id=NEW.id;
 INSERT INTO music_album_release_identities(owner,namespace,release_id,album_id)
 SELECT c.owner,c.namespace,c.release_id,coalesce((SELECT h.album_id FROM music_album_release_claims h
  WHERE h.source_id=c.id ORDER BY h.external_lookup DESC,h.album_id LIMIT 1),lower(hex(randomblob(16))))
 FROM music_source_album_choices c WHERE c.id=NEW.id AND c.override_id IS NULL AND c.namespace IS NOT NULL
 AND NOT EXISTS(SELECT 1 FROM music_album_release_identities i WHERE i.owner=c.owner AND i.namespace=c.namespace AND i.release_id=c.release_id)
 ON CONFLICT DO NOTHING;
 -- 外键在事务提交时检查；登记先决定唯一专辑，再只为首次见到的身份建卡。
 INSERT INTO music_albums(id,owner,name,origin,native_generated)
 SELECT i.album_id,c.owner,c.album,'tag',1 FROM music_source_album_choices c JOIN music_album_release_identities i
 ON i.owner=c.owner AND i.namespace=c.namespace AND i.release_id=c.release_id
 WHERE c.id=NEW.id AND c.override_id IS NULL AND NOT EXISTS(SELECT 1 FROM music_albums a WHERE a.id=i.album_id)
 ON CONFLICT(id) DO NOTHING;

 INSERT INTO music_albums(id,owner,name,origin,tag_provider,tag_name)
 SELECT lower(hex(randomblob(16))),c.owner,c.album,'tag',c.provider,c.album FROM music_source_album_choices c
 WHERE c.id=NEW.id AND c.override_id IS NULL AND c.namespace IS NULL AND c.previous_id IS NULL AND c.legacy_id IS NULL
 ON CONFLICT(owner,tag_provider,tag_name) DO NOTHING;
 INSERT INTO music_albums(id,owner,name,origin,tag_provider,unresolved_tag)
 SELECT lower(hex(randomblob(16))),u.owner,u.album,'tag',u.provider,u.album FROM music_unidentified_album_sources u
 WHERE u.id=NEW.id ON CONFLICT(owner,tag_provider,unresolved_tag) DO NOTHING;

 UPDATE music_albums SET native_displaced=1 WHERE origin='tag' AND native_displaced=0
 AND id IN (SELECT r.album_id FROM music_album_source_refs r WHERE r.source_id=NEW.id AND r.origin_id='tag')
 AND EXISTS(SELECT 1 FROM music_source_album_choices c JOIN music_album_source_destinations d ON d.id=c.id
  WHERE c.id=NEW.id AND c.namespace IS NOT NULL AND c.override_id IS NULL AND d.album_id<>music_albums.id);
 -- 让过期编辑器重新读取真实发生变化的曲目/专辑；完全相同的扫描不递增修订。
 UPDATE music_tracks SET revision=revision+1,updated_at=strftime('%Y-%m-%dT%H:%M:%fZ','now')
 WHERE id IN (SELECT track_id FROM music_album_membership_changes WHERE source_id=NEW.id);
 UPDATE music_albums SET revision=revision+1 WHERE id IN (
  SELECT album_id FROM music_album_membership_changes WHERE source_id=NEW.id);
 INSERT INTO music_album_source_refs(album_id,source_id,origin_id,position)
 SELECT d.album_id,d.id,'tag',0 FROM music_album_source_destinations d WHERE d.id=NEW.id
 ON CONFLICT(album_id,origin_id,source_id) DO NOTHING;
 DELETE FROM music_album_source_refs WHERE source_id=NEW.id AND origin_id='tag' AND NOT EXISTS(
  SELECT 1 FROM music_album_source_destinations d WHERE d.id=NEW.id AND d.album_id=music_album_source_refs.album_id);

 INSERT INTO music_album_source_tag_history(owner,source_id,tag_name,album_id)
 SELECT c.owner,c.id,c.album,r.album_id FROM music_source_album_choices c JOIN music_album_source_refs r ON r.source_id=c.id AND r.origin_id='tag'
 WHERE c.id=NEW.id AND c.override_id IS NULL
 ON CONFLICT(owner,source_id) DO UPDATE SET tag_name=excluded.tag_name,album_id=excluded.album_id
 WHERE music_album_source_tag_history.tag_name<>excluded.tag_name OR music_album_source_tag_history.album_id<>excluded.album_id;
 DELETE FROM music_album_source_tag_history WHERE source_id=NEW.id AND NOT EXISTS(
  SELECT 1 FROM music_source_album_choices c WHERE c.id=NEW.id AND c.override_id IS NULL);
 INSERT INTO music_refresh_metadata SELECT track_id FROM music_observed_tracks WHERE id=NEW.id;
END;

-- 发行号可能晚于其他字段；单独新增、更正和暂缺编号都重新投影。
DROP TRIGGER music_changed_observation;
CREATE TRIGGER music_changed_observation AFTER UPDATE OF document,provider,external_id ON tracks
WHEN OLD.provider<>NEW.provider OR OLD.external_id<>NEW.external_id OR json_valid(OLD.document)<>json_valid(NEW.document)
 OR (json_valid(OLD.document) AND json_valid(NEW.document) AND (
  json_extract(OLD.document,'$.title','$.displayName','$.artists','$.album','$.genre','$.year','$.albumRelease')
  <>json_extract(NEW.document,'$.title','$.displayName','$.artists','$.album','$.genre','$.year','$.albumRelease')))
BEGIN INSERT INTO music_import_sources VALUES(NEW.id); END;

-- 路径可被另一首歌替换；只有原始曲名/署名相容，自动历史才可延续。显式逐源覆盖独立保存。
CREATE TRIGGER music_forget_replaced_album_source BEFORE UPDATE OF document ON tracks
WHEN OLD.document<>NEW.document AND json_valid(OLD.document) AND json_valid(NEW.document) AND (
 (nullif(trim(json_extract(OLD.document,'$.title')),'') IS NOT NULL AND nullif(trim(json_extract(NEW.document,'$.title')),'') IS NOT NULL
  AND lower(trim(json_extract(OLD.document,'$.title')))<>lower(trim(json_extract(NEW.document,'$.title'))))
 OR (json_type(OLD.document,'$.artists')='array' AND json_type(NEW.document,'$.artists')='array'
  AND EXISTS(SELECT 1 FROM json_each(OLD.document,'$.artists') b WHERE b.type='text' AND trim(b.value)<>'')
  AND EXISTS(SELECT 1 FROM json_each(NEW.document,'$.artists') n WHERE n.type='text' AND trim(n.value)<>'')
  AND NOT EXISTS(SELECT 1 FROM json_each(OLD.document,'$.artists') b,json_each(NEW.document,'$.artists') n
   WHERE b.type='text' AND n.type='text' AND trim(b.value)<>'' AND lower(trim(b.value))=lower(trim(n.value)))))
BEGIN DELETE FROM music_album_source_tag_history WHERE owner=OLD.owner AND source_id=OLD.id; END;
