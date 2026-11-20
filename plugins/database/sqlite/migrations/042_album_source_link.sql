-- 专辑页面关联独立保存；保存关联本身不执行网络请求。
ALTER TABLE music_albums ADD COLUMN source_url TEXT;
ALTER TABLE music_albums ADD COLUMN source_plugin_id TEXT;
UPDATE music_albums SET
 source_url=(SELECT json_extract(f.document,'$.item.sourceUrl') FROM music_album_scraped_fields f WHERE f.album_id=music_albums.id AND f.field='albumDetails'),
 source_plugin_id=(SELECT json_extract(f.document,'$.pluginId') FROM music_album_scraped_fields f WHERE f.album_id=music_albums.id AND f.field='albumDetails')
WHERE EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=music_albums.id AND f.field='albumDetails'
 AND json_extract(f.document,'$.item.sourceUrl') LIKE 'https://%'
 AND length(coalesce(json_extract(f.document,'$.pluginId'),''))>0);
