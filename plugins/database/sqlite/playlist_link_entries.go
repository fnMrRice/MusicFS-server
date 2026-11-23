package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

// 只投递本次新加入的条目。改名、排序、重建、改选来源和关联前已有的曲目都不触发写入。
func queuePlaylistEntries(ctx context.Context, tx *sql.Tx, owner, playlistID string, old, updated []playlists.PlaylistBinding) error {
	existing := map[string]bool{}
	for _, b := range old {
		for _, e := range b.Entries {
			existing[e.ID] = true
		}
	}
	var refs []playlists.SourceRef
	for _, b := range updated {
		for _, entry := range b.Entries {
			if !existing[entry.ID] {
				refs = append(refs, entry.Source)
			}
		}
	}
	return queuePlaylistSources(ctx, tx, owner, playlistID, refs)
}

func queuePlaylistSources(ctx context.Context, tx *sql.Tx, owner, playlistID string, refs []playlists.SourceRef) error {
	if len(refs) == 0 {
		return nil
	}
	var linked bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM playlist_links WHERE owner=? AND playlist_id=?)", owner, playlistID).Scan(&linked); err != nil || !linked {
		return err
	}
	var tracks []music.Track
	auto := map[string]string{}
	for _, ref := range refs {
		if ref.TrackID != nil && ref.ForcedSourceID == nil {
			if _, seen := auto[*ref.TrackID]; !seen {
				auto[*ref.TrackID] = ""
				tracks = append(tracks, music.Track{ID: *ref.TrackID})
			}
		}
	}
	// 自动选源沿用曲库的人工默认、可用性和音质顺序，批量读取涉及的曲目。
	if err := loadMusicRelations(ctx, tx, owner, tracks, false); err != nil {
		return err
	}
	for _, t := range tracks {
		if t.SelectedSource != nil {
			auto[t.ID] = t.SelectedSource.ID
		}
	}
	type source struct {
		Local  string `json:"local,omitempty"`
		Remote string `json:"remote,omitempty"`
	}
	sources := make([]source, 0, len(refs))
	for _, ref := range refs {
		var s source
		switch {
		case ref.RemoteSourceID != nil:
			s.Remote = *ref.RemoteSourceID
		case ref.SourceTrackID != nil:
			s.Local = *ref.SourceTrackID
		case ref.ForcedSourceID != nil:
			s.Local = *ref.ForcedSourceID
		case ref.TrackID != nil:
			s.Local = auto[*ref.TrackID]
		}
		sources = append(sources, s)
	}
	raw, err := json.Marshal(sources)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `WITH additions AS (
 SELECT j.key AS pos,coalesce(m.provider_instance_id,r.instance_id) AS instance_id,
 coalesce(m.external_id,r.external_id) AS track_id, coalesce(m.plugin_id,p.plugin_id) AS plugin_id
 FROM json_each(?) j
 LEFT JOIN music_sources m ON m.owner=? AND m.id=json_extract(j.value,'$.local')
 LEFT JOIN remote_sources r ON r.owner=? AND r.id=json_extract(j.value,'$.remote')
 LEFT JOIN music_source_providers p ON p.provider_instance_id=r.instance_id)
 INSERT INTO playlist_link_additions(link_id,track_id)
 SELECT l.id,a.track_id FROM playlist_links l
 LEFT JOIN music_source_providers target ON target.provider_instance_id=l.instance_id
 JOIN additions a ON a.instance_id=l.instance_id OR a.plugin_id=target.plugin_id
 WHERE l.owner=? AND l.playlist_id=? AND a.track_id IS NOT NULL
 ORDER BY a.pos,l.id ON CONFLICT(link_id,track_id) DO NOTHING`, string(raw), owner, owner, owner, playlistID)
	return err
}
