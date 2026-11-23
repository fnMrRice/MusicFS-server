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

func saveRemotePlaylist(ctx context.Context, tx *sql.Tx, p *playlists.RemotePlaylist, insert bool) error {
	p.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(remotePlaylistRecord{*p, p.ConfigRevision})
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO remote_playlists(id,owner,instance_id,account_id,external_id,binding_revision,revision,document) VALUES(?,?,?,?,?,?,?,?)", p.ID, p.Owner, p.InstanceID, p.AccountID, p.ExternalID, p.BindingRevision, p.Revision, raw)
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE remote_playlists SET binding_revision=?,revision=?,document=? WHERE owner=? AND id=?", p.BindingRevision, p.Revision, raw, p.Owner, p.ID)
	}
	return err
}
func saveRemoteJob(ctx context.Context, tx *sql.Tx, j *playlists.SyncJob, insert bool) error {
	j.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(remoteJobRecord{*j, j.Binding})
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO remote_sync_jobs VALUES(?,?,?,?,?,?,?,?)", j.ID, j.Owner, j.PlaylistID, j.State, j.Generation, j.BindingRevision, raw, j.CreatedAt.UTC().Format(time.RFC3339Nano))
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE remote_sync_jobs SET state=?,generation=?,binding_revision=?,document=? WHERE owner=? AND id=?", j.State, j.Generation, j.BindingRevision, raw, j.Owner, j.ID)
	}
	return err
}
func remoteMutation(ctx context.Context, s *Store, owner, key string) (*sql.Tx, error) {
	if !playlists.ValidID(owner) || len(key) < 8 || len(key) > 128 {
		return nil, playlists.Invalid()
	}
	return s.DB.BeginTx(ctx, nil)
}
func saveRemoteOperation(ctx context.Context, tx *sql.Tx, owner, route, key, digest, id string) error {
	raw, _ := json.Marshal(id)
	return saveContentIdempotency(ctx, tx, owner, route, key, digest, raw)
}
func ensureRemotePlaylist(ctx context.Context, tx *sql.Tx, owner string, b playlists.Binding) (playlists.RemotePlaylist, bool, error) {
	var empty playlists.RemotePlaylist
	// An instance is a persistent source namespace. Reusing its identity for a
	// different platform cannot reinterpret already imported remote source IDs.
	var previousInstanceID string
	instanceErr := tx.QueryRowContext(ctx, "SELECT id FROM remote_playlists WHERE owner=? AND instance_id=? ORDER BY id LIMIT 1", owner, b.InstanceID).Scan(&previousInstanceID)
	if instanceErr == nil {
		previous, e := readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, previousInstanceID))
		if e != nil {
			return empty, false, e
		}
		if previous.Platform != b.Platform {
			return empty, false, domain.Fail("CONFLICT", "Remote instance platform changed")
		}
	} else if !errors.Is(instanceErr, sql.ErrNoRows) {
		return empty, false, instanceErr
	}
	var raw []byte
	var existsID string
	err := tx.QueryRowContext(ctx, "SELECT id,document FROM remote_playlists WHERE owner=? AND instance_id=? AND account_id=? AND external_id=?", owner, b.InstanceID, b.AccountID, b.ExternalID).Scan(&existsID, &raw)
	merged := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, false, err
	}
	p := playlists.RemotePlaylist{ID: domain.NewID(), Owner: owner, RemoteRef: b.RemoteRef, Platform: b.Platform, ConfigRevision: b.ConfigRevision, BindingRevision: 1, Stale: true, CreatedAt: time.Now().UTC()}
	if merged {
		p, err = readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, existsID))
		if err != nil {
			return empty, false, err
		}
		if p.Platform != b.Platform {
			return empty, false, domain.Fail("CONFLICT", "Remote instance platform changed")
		}
	} else if err = saveRemotePlaylist(ctx, tx, &p, true); err != nil {
		return empty, false, err
	}
	return p, merged, nil
}

func (s *Store) QueueRemoteSync(ctx context.Context, owner, id string, expected int64, b playlists.Binding, key string) (playlists.SyncJob, bool, error) {
	var empty playlists.SyncJob
	if expected < 1 || !playlists.ValidID(id) {
		return empty, false, playlists.Invalid()
	}
	if err := b.Validate(); err != nil {
		return empty, false, err
	}
	digest, err := playlists.Digest(struct {
		ID       string
		Expected int64
		Binding  playlists.Binding
	}{id, expected, b})
	if err != nil {
		return empty, false, err
	}
	tx, err := remoteMutation(ctx, s, owner, key)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	prior, replay, err := contentIdempotency(ctx, tx, owner, "remote-playlists/sync", key, digest)
	if err != nil {
		return empty, false, err
	}
	if replay {
		var jobID string
		if err = json.Unmarshal(prior, &jobID); err != nil {
			return empty, false, err
		}
		j, e := readRemoteJob(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", owner, jobID))
		return j, true, e
	}
	p, err := readRemotePlaylist(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_playlists WHERE owner=? AND id=?", owner, id))
	if err != nil {
		return empty, false, err
	}
	if p.BindingRevision != expected || p.RemoteRef != b.RemoteRef || p.Platform != b.Platform {
		return empty, false, domain.Fail("CONFLICT", "Remote playlist binding changed")
	}
	j, merged, err := queueCatalogSync(ctx, tx, &p, b)
	if err != nil {
		return empty, false, err
	}
	if err = recordCatalogJob(ctx, tx, owner, id, b, j.ID); err != nil {
		return empty, false, err
	}
	if err = saveRemoteOperation(ctx, tx, owner, "remote-playlists/sync", key, digest, j.ID); err != nil {
		return empty, false, err
	}
	return j, merged, tx.Commit()
}

func queueCatalogSync(ctx context.Context, tx *sql.Tx, p *playlists.RemotePlaylist, b playlists.Binding) (playlists.SyncJob, bool, error) {
	var empty playlists.SyncJob
	owner, id := p.Owner, p.ID
	var activeID string
	err := tx.QueryRowContext(ctx, "SELECT id FROM remote_sync_jobs WHERE playlist_id=? AND state IN ('queued','running')", id).Scan(&activeID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return empty, false, err
	}
	if activeID != "" && p.ConfigRevision == b.ConfigRevision {
		j, e := readRemoteJob(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", owner, activeID))
		if e != nil {
			return empty, false, e
		}
		return j, true, nil
	}
	if p.ConfigRevision != b.ConfigRevision {
		if activeID != "" {
			j, e := readRemoteJob(tx.QueryRowContext(ctx, "SELECT owner,document FROM remote_sync_jobs WHERE owner=? AND id=?", owner, activeID))
			if e != nil {
				return empty, false, e
			}
			j.State = "cancelled"
			j.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Remote instance configuration changed"}
			if err = saveRemoteJob(ctx, tx, &j, false); err != nil {
				return empty, false, err
			}
		}
		p.ConfigRevision = b.ConfigRevision
		p.BindingRevision++
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM remote_sync_jobs WHERE state IN ('queued','running')").Scan(&count); err != nil {
		return empty, false, err
	}
	if count >= 1024 {
		return empty, false, domain.Fail("RATE_LIMITED", "Remote playlist queue is full")
	}
	j := playlists.SyncJob{ID: domain.NewID(), Owner: owner, PlaylistID: id, Binding: b, BindingRevision: p.BindingRevision, State: "queued", CreatedAt: time.Now().UTC()}
	if err = saveRemoteJob(ctx, tx, &j, true); err != nil {
		return empty, false, err
	}
	p.Stale = true
	if err = saveRemotePlaylist(ctx, tx, p, false); err != nil {
		return empty, false, err
	}
	return j, false, nil
}
