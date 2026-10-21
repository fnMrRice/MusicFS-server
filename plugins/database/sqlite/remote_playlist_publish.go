package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func ensureRemoteSource(ctx context.Context, tx *sql.Tx, owner string, b playlists.Binding, external string) (playlists.RemoteSource, error) {
	var id string
	err := tx.QueryRowContext(ctx, "SELECT id FROM remote_sources WHERE owner=? AND instance_id=? AND account_id=? AND external_id=?", owner, b.InstanceID, b.AccountID, external).Scan(&id)
	if err == nil {
		return readRemoteSource(tx.QueryRowContext(ctx, "SELECT document FROM remote_sources WHERE owner=? AND id=?", owner, id))
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return playlists.RemoteSource{}, err
	}
	source := playlists.RemoteSource{ID: domain.NewID(), RemoteRef: playlists.RemoteRef{InstanceID: b.InstanceID, AccountID: b.AccountID, ExternalID: external}, PlaybackAvailability: "unsupported"}
	raw, err := json.Marshal(source)
	if err != nil {
		return source, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO remote_sources VALUES(?,?,?,?,?,?)", source.ID, owner, b.InstanceID, b.AccountID, external, raw)
	return source, err
}
func (s *Store) FinishRemoteSync(ctx context.Context, ticket playlists.SyncJob, observation playlists.Observation) (playlists.Snapshot, error) {
	var empty playlists.Snapshot
	if err := observation.Validate(ticket.Binding.ExternalID); err != nil {
		return empty, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, err
	}
	defer tx.Rollback()
	j, p, err := activeRemoteJob(ctx, tx, ticket)
	if err != nil {
		return empty, err
	}
	var previous playlists.Snapshot
	if p.Revision > 0 {
		previous, err = readRemoteSnapshot(ctx, tx, j.Owner, p.ID, p.Revision)
		if err != nil {
			return empty, err
		}
	}
	sources := map[string]playlists.RemoteSource{}
	for _, entry := range observation.Entries {
		if _, ok := sources[entry.ExternalTrackID]; ok {
			continue
		}
		source, e := ensureRemoteSource(ctx, tx, j.Owner, j.Binding, entry.ExternalTrackID)
		if e != nil {
			return empty, e
		}
		sources[entry.ExternalTrackID] = source
	}
	next, err := playlists.Reconcile(previous, observation, playlists.Snapshot{PlaylistID: p.ID, Revision: p.Revision + 1, BindingRevision: p.BindingRevision, ObservationID: domain.NewID()}, sources)
	if err != nil {
		return empty, err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		return empty, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO remote_playlist_snapshots VALUES(?,?,?,?)", p.ID, next.Revision, p.Owner, raw); err != nil {
		return empty, err
	}
	p.Revision = next.Revision
	p.Title = observation.Playlist.Title
	p.Stale = false
	p.Error = nil
	now := time.Now().UTC()
	p.LastSuccessAt = &now
	j.State = "succeeded"
	j.Error = nil
	j.ResultRevision = &next.Revision
	if err = saveRemotePlaylist(ctx, tx, &p, false); err != nil {
		return empty, err
	}
	if err = saveRemoteJob(ctx, tx, &j, false); err != nil {
		return empty, err
	}
	return next, tx.Commit()
}

var _ playlists.Repository = (*Store)(nil)
