package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/playlists"
)

func playlistSourceVisible(ctx context.Context, tx *sql.Tx, owner string, source playlists.SourceRef) error {
	if err := source.Validate(); err != nil {
		return err
	}
	table, id := "tracks", source.SourceTrackID
	if source.RemoteSourceID != nil {
		table, id = "remote_sources", source.RemoteSourceID
	}
	var found string
	err := tx.QueryRowContext(ctx, "SELECT id FROM "+table+" WHERE owner=? AND id=?", owner, *id).Scan(&found)
	return remoteNotFound(err)
}

func playlistSourceGroup(ctx context.Context, tx *sql.Tx, owner string, source playlists.SourceRef) (*string, []playlists.SourceRef, error) {
	if err := source.Validate(); err != nil {
		return nil, nil, err
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
