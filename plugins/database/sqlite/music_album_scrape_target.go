package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func (s *Store) MusicAlbumScrapeTarget(ctx context.Context, owner, id, provider string) (music.AlbumScrapeTarget, error) {
	var result music.AlbumScrapeTarget
	if !works.ValidID(id) || provider != "" && !works.ValidID(provider) {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid album or provider identifier")
	}
	// 优先采用导入该专辑的来源。手工组建的专辑按可见曲目数决定来源；界面指定实例时只统计该实例。
	var release audiotypes.AlbumRelease
	err := s.DB.QueryRowContext(ctx, musicCategoryScope+`, target AS (
 SELECT a.name,coalesce(a.tag_provider,sa.provider_id,'') AS original_provider,i.namespace,i.release_id FROM music_albums a
 LEFT JOIN source_albums sa ON sa.id=a.id AND sa.owner=a.owner
 LEFT JOIN music_album_release_identities i ON i.owner=a.owner AND i.album_id=a.id
 WHERE a.owner=? AND a.id=? AND `+ordinaryMusicAlbumMetadata+`
), members AS (
 SELECT x.track_id FROM music_album_tracks x JOIN scope ON scope.id=x.track_id WHERE x.album_id=?
), sources AS (
 SELECT s.plugin_id,s.track_id,CASE WHEN s.provider_instance_id=target.original_provider THEN 1 ELSE 0 END AS original
 FROM target,members JOIN visible_music_sources v ON v.track_id=members.track_id
 JOIN music_sources s ON s.id=v.id WHERE (?='' OR s.provider_instance_id=?)
)
 SELECT name,coalesce((SELECT name FROM members JOIN music_track_artists c ON c.track_id=members.track_id
 JOIN music_artists ar ON ar.id=c.artist_id GROUP BY ar.id ORDER BY count(*) DESC,min(c.position),ar.id LIMIT 1),''),
 coalesce((SELECT plugin_id FROM sources GROUP BY plugin_id ORDER BY max(original) DESC,count(DISTINCT track_id) DESC,plugin_id LIMIT 1),''),
 coalesce(target.namespace,''),coalesce(target.release_id,'')
 FROM target WHERE (?='' OR EXISTS(SELECT 1 FROM members))`,
		owner, owner, owner, provider, provider, owner, id, id, provider, provider, provider).
		Scan(&result.Name, &result.Artist, &result.SourcePluginID, &release.Namespace, &release.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.Fail("NOT_FOUND", "Music album not found for this source")
	}
	if err == nil && release.ID != "" {
		result.Release = &release
	}
	return result, err
}
