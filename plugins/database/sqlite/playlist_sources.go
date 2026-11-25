package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"maps"
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type playlistSourceIDs map[string]map[string]bool

func (ids playlistSourceIDs) add(source playlists.SourceRef) error {
	if err := source.Validate(); err != nil {
		return err
	}
	table, id := "tracks", source.SourceTrackID
	if source.TrackID != nil {
		table, id = "music_tracks", source.TrackID
	}
	if source.RemoteSourceID != nil {
		table, id = "remote_sources", source.RemoteSourceID
	}
	if ids[table] == nil {
		ids[table] = make(map[string]bool)
	}
	ids[table][*id] = true
	return nil
}

// 每种来源只查询一次。按请求的标识查主键，避免扫描用户的整个曲库。
func (ids playlistSourceIDs) checkVisible(ctx context.Context, tx *sql.Tx, owner string) error {
	for table, group := range ids {
		raw, err := json.Marshal(slices.Collect(maps.Keys(group)))
		if err != nil {
			return err
		}
		var missing bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM json_each(?) AS requested
			WHERE NOT EXISTS(SELECT 1 FROM `+table+` AS source WHERE source.id=requested.value AND source.owner=?))`,
			string(raw), owner).Scan(&missing)
		if err != nil {
			return err
		}
		if missing {
			return remoteNotFound(sql.ErrNoRows)
		}
	}
	return nil
}

func playlistSourceGroup(ctx context.Context, tx *sql.Tx, owner string, source playlists.SourceRef) (*string, []playlists.SourceRef, error) {
	if err := source.Validate(); err != nil {
		return nil, nil, err
	}
	if source.TrackID != nil {
		var found int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM music_tracks WHERE owner=? AND id=?", owner, *source.TrackID).Scan(&found); err != nil {
			return nil, nil, remoteNotFound(err)
		}
		if source.ForcedSourceID == nil {
			return nil, []playlists.SourceRef{source}, nil
		}
		// 指定版本的隐藏规则只覆盖已确认的同曲版本。已被拆出的版本仍按原条目处理。
		rows, err := tx.QueryContext(ctx, `SELECT id FROM music_sources WHERE owner=? AND track_id=?
			AND EXISTS(SELECT 1 FROM music_sources WHERE owner=? AND track_id=? AND id=?) ORDER BY id LIMIT 10001`,
			owner, *source.TrackID, owner, *source.TrackID, *source.ForcedSourceID)
		if err != nil {
			return nil, nil, err
		}
		defer rows.Close()
		members := []playlists.SourceRef{}
		for rows.Next() {
			var member string
			if err = rows.Scan(&member); err != nil {
				return nil, nil, err
			}
			members = append(members, playlists.SourceRef{TrackID: source.TrackID, ForcedSourceID: &member})
		}
		if len(members) > playlists.MaxEntries {
			return nil, nil, domain.Fail("RATE_LIMITED", "Recording member set exceeds playlist limit")
		}
		if len(members) == 0 {
			members = append(members, source)
		}
		return nil, members, rows.Err()
	}
	if source.RemoteSourceID != nil {
		if _, err := readRemoteSource(tx.QueryRowContext(ctx, "SELECT document FROM remote_sources WHERE owner=? AND id=?", owner, *source.RemoteSourceID)); err != nil {
			return nil, nil, err
		}
		return nil, []playlists.SourceRef{source}, nil
	}
	var id string
	err := tx.QueryRowContext(ctx, "SELECT id FROM tracks WHERE owner=? AND id=?", owner, *source.SourceTrackID).Scan(&id)
	if err != nil {
		return nil, nil, remoteNotFound(err)
	}
	var recording sql.NullString
	err = tx.QueryRowContext(ctx, "SELECT l.recording_id FROM recording_source_links l JOIN recordings r ON r.id=l.recording_id AND r.owner=l.owner WHERE l.owner=? AND l.source_id=?", owner, id).Scan(&recording)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !recording.Valid {
		return nil, []playlists.SourceRef{source}, nil
	}
	if err != nil {
		return nil, nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT l.source_id FROM recording_source_links l JOIN tracks t ON t.id=l.source_id AND t.owner=l.owner WHERE l.owner=? AND l.recording_id=? ORDER BY l.source_id LIMIT 10001", owner, recording.String)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	group := []playlists.SourceRef{}
	for rows.Next() {
		var member string
		if err = rows.Scan(&member); err != nil {
			return nil, nil, err
		}
		group = append(group, playlists.SourceRef{SourceTrackID: &member})
	}
	if len(group) > playlists.MaxEntries {
		return nil, nil, domain.Fail("RATE_LIMITED", "Recording member set exceeds playlist limit")
	}
	return &recording.String, group, rows.Err()
}
