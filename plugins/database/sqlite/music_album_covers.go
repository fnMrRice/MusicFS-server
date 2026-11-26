package sqlite

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

var albumMediaPath = regexp.MustCompile(`^/api/v1/media/[A-Za-z0-9_-]+$`)

// 专辑封面按已选平台资料、专辑原封面、普通曲目和可见来源回退；一次查询覆盖整页。
func populateMusicAlbumCovers(ctx context.Context, query trackCoverQuery, owner string, albums ...*music.Album) error {
	byID := map[string][]*music.Album{}
	ids := []string{}
	for _, album := range albums {
		if album.ID == "" {
			continue
		}
		if _, exists := byID[album.ID]; !exists {
			ids = append(ids, album.ID)
		}
		byID[album.ID] = append(byID[album.ID], album)
		album.CoverPath = nil
	}
	if len(ids) == 0 {
		return nil
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	// 本页专辑共享候选。排除播放时自动写回的网易云图片，保留可刷新的来源以及用户手选图片的优先级。
	rows, err := query.QueryContext(ctx, musicVisible+`, target_albums AS MATERIALIZED (
 SELECT a.id,a.owner,a.cover_path,a.metadata_locked FROM json_each(?) ids CROSS JOIN music_albums a ON a.id=ids.value WHERE a.owner=?
), target_members AS MATERIALIZED (
 SELECT x.album_id,x.position,x.track_id FROM music_album_tracks x WHERE x.album_id IN (SELECT id FROM target_albums)
), target_sources AS MATERIALIZED (
 SELECT id,track_id FROM visible_music_sources WHERE track_id IN (SELECT track_id FROM target_members)
), online_sources AS MATERIALIZED (
 SELECT s.id,s.track_id,n.owner FROM target_sources s JOIN music_sources n ON n.id=s.id
 JOIN tracks t ON t.id=s.id AND t.owner=n.owner
 WHERE n.plugin_id=? AND t.root_id='online-audio' AND json_extract(t.document,'$.availability')='available'
 AND coalesce(json_extract(t.document,'$._providerCoverUrl'),'legacy')<>''
), automatic_online_art AS MATERIALIZED (
 SELECT o.id,'/api/v1/media/'||o.id AS path FROM online_sources s
 JOIN media_content_slots h ON h.source_id=s.id AND h.owner=s.owner AND h.field='artwork' AND h.language='' AND h.manual_locked=0
 JOIN media_content_bindings b ON b.id=h.binding_id AND b.current=1
 JOIN media_content_objects o ON o.id=b.object_id AND o.source_id=s.id AND o.owner=s.owner AND o.state='available'
 WHERE json_extract(o.document,'$.metadata.origin.kind')='import'
 AND NOT EXISTS(SELECT 1 FROM recording_cover_preferences p WHERE p.owner=s.owner AND p.recording_id=s.track_id AND p.object_id=o.id)
), candidates AS (
 SELECT a.id AS album_id,CASE f.field WHEN 'artwork' THEN 0 ELSE 1 END AS priority,
  json_extract(f.document,'$.item.imageUrl') AS path,0 AS position,f.field AS identity
 FROM target_albums a JOIN music_album_scraped_fields f ON f.album_id=a.id WHERE f.field IN ('artwork','albumDetails')
 UNION ALL SELECT a.id,2,a.cover_path,0,a.id FROM target_albums a
 WHERE a.metadata_locked=1 OR a.cover_path NOT IN (SELECT path FROM automatic_online_art)
 OR EXISTS(SELECT 1 FROM album_cover_heads h WHERE h.owner=a.owner AND h.target_kind='sourceAlbum' AND h.target_id=a.id AND h.manual_locked=1)
 UNION ALL SELECT x.album_id,3,m.cover_path,x.position,m.id FROM target_members x CROSS JOIN music_tracks m
 ON m.id=x.track_id AND m.owner=? WHERE m.cover_path IS NOT NULL AND m.cover_path<>''
 AND (m.metadata_locked=1 OR m.cover_path NOT IN (SELECT path FROM automatic_online_art))
 AND EXISTS(SELECT 1 FROM visible_music_tracks v WHERE v.id=m.id)
 UNION ALL SELECT x.album_id,4,'/api/v1/media/'||c.object_id,x.position,c.object_id FROM target_members x
 JOIN target_sources s ON s.track_id=x.track_id JOIN music_track_covers c ON c.track_id=x.track_id
 JOIN media_content_objects o ON o.id=c.object_id AND o.owner=? AND o.source_id=s.id
 WHERE o.id NOT IN (SELECT id FROM automatic_online_art)
 UNION ALL SELECT x.album_id,5,s.id,x.position,s.id FROM target_members x JOIN online_sources s ON s.track_id=x.track_id
)
 SELECT c.album_id,c.path,c.priority FROM candidates c WHERE c.path IS NOT NULL AND c.path<>'' AND (
 lower(c.path) NOT LIKE '/api/v1/media/%' OR EXISTS(SELECT 1 FROM media_content_objects o
 JOIN tracks t ON t.id=o.source_id AND t.owner=o.owner
 JOIN visible_music_sources s ON s.id=t.id
 WHERE o.owner=? AND o.id=substr(c.path,15) AND o.state='available'
 AND json_extract(t.document,'$.availability')='available'))
 ORDER BY c.album_id,c.priority,c.position,c.identity`, owner, owner, owner, string(raw), owner, pluginproto.OnlinePluginID(online.Netease), owner, owner, owner)
	if err != nil {
		return err
	}
	defer rows.Close()
	chosen := map[string]bool{}
	for rows.Next() {
		var id, path string
		var priority int
		if err = rows.Scan(&id, &path, &priority); err != nil {
			return err
		}
		if chosen[id] {
			continue
		}
		if priority == 5 {
			// 在线封面只发布可见来源的服务端端点。
			path = "/api/v1/source-tracks/" + url.PathEscape(path) + "/online-artwork"
		} else if !usableAlbumCoverPath(path) {
			continue
		}
		chosen[id] = true
		for _, album := range byID[id] {
			value := path
			album.CoverPath = &value
		}
	}
	return rows.Err()
}

func usableAlbumCoverPath(path string) bool {
	if albumMediaPath.MatchString(path) {
		return true
	}
	parsed, err := url.Parse(path)
	return err == nil && parsed.Scheme == "https" && parsed.Hostname() != "" && parsed.User == nil &&
		!strings.ContainsAny(path, "\r\n\t ")
}
