package sqlite

import (
	"context"
	"database/sql"
	"reflect"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func activeRemoteJob(ctx context.Context, tx *sql.Tx, ticket playlists.SyncJob) (playlists.SyncJob, playlists.RemotePlaylist, error) {
	var p playlists.RemotePlaylist
	j, err := readRemoteJob(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", ticket.Owner, ticket.ID))
	if err != nil {
		return j, p, err
	}
	if j.State != "running" || ticket.State != "running" || j.PlaylistID != ticket.PlaylistID || j.Generation != ticket.Generation || j.BindingRevision != ticket.BindingRevision || !reflect.DeepEqual(j.Binding, ticket.Binding) {
		return j, p, domain.Fail("CONFLICT", "Remote playlist task generation changed")
	}
	p, err = readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", j.Owner, j.PlaylistID))
	if err != nil {
		return j, p, err
	}
	if p.BindingRevision != j.BindingRevision || p.ConfigRevision != j.Binding.ConfigRevision || p.RemoteRef != j.Binding.RemoteRef || p.Platform != j.Binding.Platform {
		return j, p, domain.Fail("CONFLICT", "Remote playlist binding changed")
	}
	return j, p, nil
}
func (s *Store) ClaimRemoteSync(ctx context.Context) (playlists.SyncJob, error) {
	var empty playlists.SyncJob
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	var id, owner string
	if err = tx.QueryRowContext(ctx, "SELECT id,owner FROM remote_sync_jobs WHERE state='queued' ORDER BY julianday(created_at),id LIMIT 1").Scan(&id, &owner); err != nil {
		return empty, err
	}
	j, err := readRemoteJob(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return empty, err
	}
	j.State = "running"
	j.Generation++
	if err = saveRemoteJob(ctx, tx, &j, false); err != nil {
		return empty, err
	}
	return j, tx.Commit()
}
func (s *Store) CancelRemoteSync(ctx context.Context, owner, id string) (playlists.SyncJob, error) {
	var empty playlists.SyncJob
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	j, err := readRemoteJob(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return empty, err
	}
	if j.State != "queued" && j.State != "running" {
		return j, nil
	}
	j.State = "cancelled"
	j.Error = &domain.Error{Code: "CANCELED", Message: "Remote playlist synchronization was cancelled"}
	if err = saveRemoteJob(ctx, tx, &j, false); err != nil {
		return empty, err
	}
	p, err := readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, j.PlaylistID))
	if err != nil {
		return empty, err
	}
	p.Stale = true
	p.Error = j.Error
	if err = saveRemotePlaylist(ctx, tx, &p, false); err != nil {
		return empty, err
	}
	return j, tx.Commit()
}
func (s *Store) FailRemoteSync(ctx context.Context, ticket playlists.SyncJob, failure *domain.Error) error {
	if failure == nil {
		return playlists.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, p, err := activeRemoteJob(ctx, tx, ticket)
	if err != nil {
		return err
	}
	// 上游消息可能含有私有传输细节，只保留公开错误码和限流等待秒数。
	safe := &domain.Error{Code: playlists.FailureCode(failure), Message: "Remote playlist synchronization failed; previous snapshot retained"}
	if safe.Code == "RATE_LIMITED" && failure.RetryAfter != nil && *failure.RetryAfter > 0 {
		seconds := *failure.RetryAfter
		safe.RetryAfter = &seconds
	}
	failure = safe
	j.State = "failed"
	j.Error = failure
	p.Stale = true
	p.Error = failure
	if err = saveRemoteJob(ctx, tx, &j, false); err != nil {
		return err
	}
	if err = saveRemotePlaylist(ctx, tx, &p, false); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) RecoverRemoteSync(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE state='running' LIMIT 1025")
	if err != nil {
		return err
	}
	jobs := []playlists.SyncJob{}
	for rows.Next() {
		j, e := readRemoteJob(rows)
		if e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if len(jobs) > 1024 {
		return domain.Fail("RATE_LIMITED", "Remote playlist recovery exceeds its limit")
	}
	for _, j := range jobs {
		j.State = "queued"
		j.Generation++
		if err = saveRemoteJob(ctx, tx, &j, false); err != nil {
			return err
		}
	}
	return tx.Commit()
}
