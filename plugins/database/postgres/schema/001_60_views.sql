CREATE VIEW music_observed_tracks AS
 SELECT t.id,t.owner,t.provider,t.external_id,coalesce(l.recording_id,t.id) AS track_id,
  coalesce(nullif(trim(t.data->>'title'),''),
           nullif(trim(t.data->>'displayName'),''),nullif(trim(t.path),''),nullif(trim(t.external_id),''),t.id) AS title,
  nullif(trim(t.data->>'genre'),'') AS genre, (t.data->>'year')::bigint AS year,
  CASE WHEN jsonb_typeof(t.data->'artists')='array' THEN t.data->'artists' END AS artists,
  nullif(trim(t.data->>'album'),'') AS album
 FROM (SELECT id,owner,provider,external_id,path,musicfs_parse_json(document) AS data FROM tracks) t
 LEFT JOIN recording_source_links l ON l.owner=t.owner AND l.source_id=t.id;

CREATE VIEW music_import_sources AS SELECT id FROM tracks;

CREATE VIEW music_refresh_metadata AS SELECT id FROM music_tracks;

CREATE VIEW music_imported_credits AS
 SELECT m.id AS track_id,m.owner,trim(a.value #>> '{}') AS name,min(a.ord)-1 AS position
 FROM music_tracks m JOIN music_observed_tracks o ON o.id=m.metadata_source_id AND o.track_id=m.id
 CROSS JOIN LATERAL jsonb_array_elements(o.artists) WITH ORDINALITY AS a(value,ord)
 WHERE m.metadata_locked=0 AND jsonb_typeof(a.value)='string' AND length(trim(a.value #>> '{}'))>0
 GROUP BY m.id,trim(a.value #>> '{}');

CREATE VIEW music_import_cd_albums AS SELECT id FROM works WHERE content_type='cd_album';

CREATE VIEW music_import_cd_packages AS SELECT id FROM work_packages;

CREATE VIEW music_track_covers AS
 SELECT s.track_id,b.object_id,p.object_id AS preferred,
  coalesce((o.document::jsonb->>'width')::bigint,0) AS width,
  coalesce((o.document::jsonb->>'height')::bigint,0) AS height
 FROM music_sources s JOIN tracks t ON t.id=s.id AND t.owner=s.owner
 JOIN source_assets a ON a.owner=t.owner AND a.id=t.asset_id AND a.track_id=t.id AND a.version=t.version AND a.current=1
 JOIN media_content_slots h ON h.owner=t.owner AND h.source_id=t.id AND h.field='artwork' AND h.language=''
 JOIN media_content_bindings b ON b.id=h.binding_id AND b.owner=h.owner AND b.source_id=h.source_id AND b.asset_id=t.asset_id
  AND b.field=h.field AND b.language=h.language AND b.current=1 AND (b.document::jsonb #>> '{target,assetVersion}')=t.version
 JOIN media_content_objects o ON o.id=b.object_id AND o.owner=b.owner AND o.source_id=t.id AND o.state='available'
 LEFT JOIN recording_cover_preferences p ON p.owner=s.owner AND p.recording_id=s.track_id
 WHERE (musicfs_parse_json(t.document)->>'availability')='available';

CREATE VIEW music_refresh_cover AS SELECT id FROM music_tracks;

CREATE VIEW music_refresh_album_cover AS SELECT id FROM music_albums;

CREATE VIEW music_album_tracks AS
 SELECT album_id,track_id,coalesce(
  (SELECT r.position FROM music_album_scraped_track_refs r WHERE r.album_id=members.album_id AND r.track_id=members.track_id),
  (SELECT max(r.position)+1 FROM music_album_scraped_track_refs r WHERE r.album_id=members.album_id)+min(position),
  min(position)) AS position
 FROM (
  SELECT album_id,track_id,position FROM music_manual_album_tracks
  UNION ALL SELECT a.album_id,s.track_id,a.position FROM music_album_source_refs a
   JOIN music_sources s ON s.id=a.source_id JOIN music_albums b ON b.id=a.album_id AND b.owner=s.owner
 ) members GROUP BY album_id,track_id;
