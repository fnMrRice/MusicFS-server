package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

const playlistLinkColumns = `l.instance_id,l.account_id,l.external_id,l.platform,l.playlist_id,
 coalesce((SELECT p.id FROM remote_playlists p WHERE p.owner=l.owner AND p.instance_id=l.instance_id AND p.account_id=l.account_id AND p.external_id=l.external_id),''),coalesce(l.catalog_job_id,''),
 (SELECT count(*) FROM playlist_link_additions a WHERE a.link_id=l.id),
 coalesce((SELECT last_error FROM playlist_link_additions a WHERE a.link_id=l.id AND last_error<>'' ORDER BY retry_at DESC LIMIT 1),'')`

func scanPlaylistLink(row interface{ Scan(...any) error }) (playlists.PlaylistLink, error) {
	var p playlists.PlaylistLink
	err := row.Scan(&p.InstanceID, &p.AccountID, &p.ExternalID, &p.Platform, &p.PlaylistID, &p.RemotePlaylistID, &p.JobID, &p.PendingCount, &p.LastErrorCode)
	return p, err
}

func (s *Store) PlaylistLinks(ctx context.Context, owner string) ([]playlists.PlaylistLink, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+playlistLinkColumns+` FROM playlist_links l WHERE l.owner=? ORDER BY l.id`, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []playlists.PlaylistLink{}
	for rows.Next() {
		link, err := scanPlaylistLink(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, link)
	}
	return items, rows.Err()
}

func (s *Store) LinkPlaylist(ctx context.Context, owner string, b playlists.Binding, playlistID string) (playlists.PlaylistLink, error) {
	var result playlists.PlaylistLink
	if b.Validate() != nil || b.AccountID == "" || !playlists.ValidID(playlistID) {
		return result, playlists.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if playlistID != personal.Favorites {
		var exists int
		if err = tx.QueryRowContext(ctx, "SELECT 1 FROM personal_playlists WHERE owner=? AND id=?", owner, playlistID).Scan(&exists); err != nil {
			return result, remoteNotFound(err)
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO playlist_links(owner,platform,instance_id,account_id,external_id,personal_playlist_id)
 VALUES(?,?,?,?,?,nullif(?,'favorites')) ON CONFLICT(platform,external_id) DO NOTHING`, owner, b.Platform, b.InstanceID, b.AccountID, b.ExternalID, playlistID); err != nil {
		return result, err
	}
	result, err = scanPlaylistLink(tx.QueryRowContext(ctx, `SELECT `+playlistLinkColumns+` FROM playlist_links l
 WHERE l.owner=? AND l.platform=? AND l.external_id=? AND l.instance_id=? AND l.account_id=? AND l.playlist_id=?`,
		owner, b.Platform, b.ExternalID, b.InstanceID, b.AccountID, playlistID))
	if errors.Is(err, sql.ErrNoRows) {
		return playlists.PlaylistLink{}, domain.Fail("CONFLICT", "Remote playlist already has a link")
	}
	if err != nil {
		return result, err
	}
	remote, _, err := schedulePlaylistCatalog(ctx, tx, owner, b)
	if err != nil {
		return result, err
	}
	result.RemotePlaylistID = remote.ID
	result.JobID = remote.JobID
	return result, tx.Commit()
}

func (s *Store) UnlinkPlaylist(ctx context.Context, owner string, ref playlists.RemoteRef, playlistID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var local string
	err = tx.QueryRowContext(ctx, `SELECT playlist_id FROM playlist_links WHERE owner=? AND instance_id=? AND account_id=? AND external_id=?`, owner, ref.InstanceID, ref.AccountID, ref.ExternalID).Scan(&local)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if local != playlistID {
		return domain.Fail("CONFLICT", "Remote playlist link changed")
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM playlist_links WHERE owner=? AND instance_id=? AND account_id=? AND external_id=? AND playlist_id=?`, owner, ref.InstanceID, ref.AccountID, ref.ExternalID, playlistID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) NextPlaylistAddition(ctx context.Context) (*playlists.LinkAddition, error) {
	var a playlists.LinkAddition
	err := s.DB.QueryRowContext(ctx, `SELECT a.id,l.owner,l.instance_id,l.account_id,l.external_id,a.track_id,a.attempts
 FROM playlist_link_additions a JOIN playlist_links l ON l.id=a.link_id
 WHERE a.retry_at<=unixepoch() ORDER BY a.retry_at,a.id LIMIT 1`).Scan(&a.ID, &a.Owner, &a.InstanceID, &a.AccountID, &a.ExternalID, &a.TrackID, &a.Attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &a, err
}

func (s *Store) FinishPlaylistAddition(ctx context.Context, id int64, code string, retryAt int64) error {
	if code == "" {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM playlist_link_additions WHERE id=?`, id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE playlist_link_additions SET attempts=attempts+1,retry_at=?,last_error=? WHERE id=?`, retryAt, code, id)
	return err
}
