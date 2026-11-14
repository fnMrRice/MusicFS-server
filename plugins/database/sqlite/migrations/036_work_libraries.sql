-- 独立作品库默认不向普通曲目列表展示音轨；可选展示的专辑保存所有者选择。
ALTER TABLE works ADD COLUMN show_in_track_list INTEGER NOT NULL DEFAULT 0 CHECK(show_in_track_list IN (0,1));
UPDATE works SET document=json_set(document,'$.showInTrackList',json('false'));

-- 将既有音乐抓轨作品转为 CD 专辑，保留作品、音轨、副本与显式引用的身份。
UPDATE works SET content_type='cd_album', revision=revision+1,
 document=json_set(document,'$.contentType','cd_album','$.revision',revision+1,'$.updatedAt',strftime('%Y-%m-%dT%H:%M:%fZ','now'))
WHERE content_type='music' AND EXISTS (
 SELECT 1 FROM work_packages p WHERE p.owner=works.owner AND p.work_id=works.id AND p.kind='cd_dump'
);

UPDATE library_revision SET revision=revision+1 WHERE changes()>0;

-- 旧建档、编辑回执沿用各自记录的资料与修订，重放时仍能跳转到迁移后的作品库。
UPDATE idempotency AS i SET result=json_set(result,'$.contentType','cd_album','$.showInTrackList',json('false'))
WHERE i.route LIKE 'works/%' AND json_valid(i.result)
 AND json_extract(i.result,'$.contentType')='music' AND EXISTS (
 SELECT 1 FROM works w WHERE w.owner=i.owner AND w.content_type='cd_album'
 AND w.id=json_extract(i.result,'$.workId') AND i.route IN ('works/create','works/'||w.id||'/update')
);
