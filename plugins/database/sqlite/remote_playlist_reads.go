package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type remotePlaylistRecord struct {
	Playlist       playlists.RemotePlaylist
	ConfigRevision string
}
type remoteJobRecord struct {
	Job     playlists.SyncJob
	Binding playlists.Binding
}

func readRemotePlaylist(row rowScanner) (playlists.RemotePlaylist, error) {
	var r remotePlaylistRecord
	var owner string
	var raw []byte
	if err := row.Scan(&owner, &raw); err != nil {
		return r.Playlist, remoteNotFound(err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r.Playlist, err
	}
	r.Playlist.Owner, r.Playlist.ConfigRevision = owner, r.ConfigRevision
	return r.Playlist, nil
}
func remoteNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Remote playlist or task not found")
	}
	return err
}
func readRemoteJob(row rowScanner) (playlists.SyncJob, error) {
	var r remoteJobRecord
	var owner string
	var raw []byte
	if err := row.Scan(&owner, &raw); err != nil {
		return r.Job, remoteNotFound(err)
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r.Job, err
	}
	r.Job.Owner, r.Job.Binding = owner, r.Binding
	return r.Job, nil
}
func (s *Store) RemotePlaylist(ctx context.Context, owner, id string) (playlists.RemotePlaylist, error) {
	return readRemotePlaylist(s.DB.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, id))
}
func (s *Store) RemotePlaylists(ctx context.Context, owner, after string, limit int) ([]playlists.RemotePlaylist, error) {
	if limit < 1 || limit > playlists.MaxPageSize+1 || (after != "" && !playlists.ValidID(after)) {
		return nil, playlists.Invalid()
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id>? ORDER BY id LIMIT ?", owner, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []playlists.RemotePlaylist{}
	for rows.Next() {
		p, e := readRemotePlaylist(rows)
		if e != nil {
			return nil, e
		}
		result = append(result, p)
	}
	return result, rows.Err()
}
func (s *Store) RemoteSyncJob(ctx context.Context, owner, id string) (playlists.SyncJob, error) {
	return readRemoteJob(s.DB.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", owner, id))
}
func readRemoteSnapshot(ctx context.Context, tx *sql.Tx, owner, id string, revision int64) (playlists.Snapshot, error) {
	var result playlists.Snapshot
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM remote_playlist_snapshots WHERE owner=? AND playlist_id=? AND revision=?", owner, id, revision).Scan(&raw)
	if err != nil {
		return result, remoteNotFound(err)
	}
	err = json.Unmarshal(raw, &result)
	return result, err
}
func (s *Store) RemoteSnapshot(ctx context.Context, owner, id string, revision int64) (playlists.Snapshot, error) {
	if revision < 1 {
		return playlists.Snapshot{}, playlists.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return playlists.Snapshot{}, err
	}
	defer tx.Rollback()
	if _, err = readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, id)); err != nil {
		return playlists.Snapshot{}, err
	}
	return readRemoteSnapshot(ctx, tx, owner, id, revision)
}
func readRemoteSource(row rowScanner) (playlists.RemoteSource, error) {
	var result playlists.RemoteSource
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return result, remoteNotFound(err)
	}
	err := json.Unmarshal(raw, &result)
	return result, err
}
func (s *Store) RemoteSource(ctx context.Context, owner, id string) (playlists.RemoteSource, error) {
	return readRemoteSource(s.DB.QueryRowContext(ctx, "SELECT document FROM remote_sources WHERE owner=? AND id=?", owner, id))
}
