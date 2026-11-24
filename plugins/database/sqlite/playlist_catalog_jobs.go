package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

func schedulePlaylistCatalog(ctx context.Context, tx *sql.Tx, owner string, b playlists.Binding) (playlists.RemotePlaylist, bool, error) {
	var result playlists.RemotePlaylist
	p, merged, err := ensureRemotePlaylist(ctx, tx, owner, b)
	if err != nil {
		return result, false, err
	}
	job, _, err := queueCatalogSync(ctx, tx, &p, b)
	if err != nil {
		return result, false, err
	}
	if err = recordCatalogJob(ctx, tx, owner, p.ID, b, job.ID); err != nil {
		return result, false, err
	}
	p.JobID = job.ID
	return p, merged, nil
}

func recordCatalogJob(ctx context.Context, tx *sql.Tx, owner, id string, b playlists.Binding, job string) error {
	if _, err := tx.ExecContext(ctx, "UPDATE remote_playlists SET catalog_job_id=? WHERE owner=? AND id=?", job, owner, id); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE playlist_links SET catalog_job_id=? WHERE owner=? AND instance_id=? AND account_id=? AND external_id=?`, job, owner, b.InstanceID, b.AccountID, b.ExternalID)
	return err
}

func (s *Store) ImportRemotePlaylist(ctx context.Context, owner string, b playlists.Binding, key string) (playlists.RemotePlaylist, bool, error) {
	var result playlists.RemotePlaylist
	if err := b.Validate(); err != nil {
		return result, false, err
	}
	digest, err := playlists.Digest(b)
	if err != nil {
		return result, false, err
	}
	tx, err := remoteMutation(ctx, s, owner, key)
	if err != nil {
		return result, false, err
	}
	defer tx.Rollback()
	const route = "remote-playlists/import-catalog"
	prior, replay, err := contentIdempotency(ctx, tx, owner, route, key, digest)
	if err != nil {
		return result, false, err
	}
	if replay {
		err = json.Unmarshal(prior, &result)
		result.Owner, result.ConfigRevision = owner, b.ConfigRevision
		return result, true, err
	}
	result, merged, err := schedulePlaylistCatalog(ctx, tx, owner, b)
	if err != nil {
		return result, false, err
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return result, false, err
	}
	if err = saveContentIdempotency(ctx, tx, owner, route, key, digest, raw); err != nil {
		return result, false, err
	}
	return result, merged, tx.Commit()
}

// 空闲时补齐未安排的歌单及已过冷却期的限流失败。鉴权失败等对应账号重新就绪。
func (s *Store) PreparePlaylistCatalog(ctx context.Context, owner, instance, platform, revision, account string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var bindings []playlists.Binding
	err = musicRows(ctx, tx, `WITH candidates AS (
		SELECT account_id,external_id,catalog_job_id FROM remote_playlists WHERE owner=? AND instance_id=?
		UNION SELECT account_id,external_id,catalog_job_id FROM playlist_links WHERE owner=? AND instance_id=? AND platform=?)
		SELECT DISTINCT c.account_id,c.external_id FROM candidates c LEFT JOIN remote_sync_jobs j ON j.id=c.catalog_job_id
		WHERE (?='' OR c.account_id=?) AND (c.catalog_job_id IS NULL OR (j.state='failed' AND (
			(json_extract(j.document,'$.Job.error.code')='RATE_LIMITED'
			AND julianday('now') >= julianday(json_extract(j.document,'$.Job.updatedAt')) + max(60,coalesce(CAST(json_extract(j.document,'$.Job.error.retryAfter') AS BIGINT),0))/86400.0)
			OR (?<>'' AND json_extract(j.document,'$.Job.error.code') IN ('AUTH_REQUIRED','PLATFORM_AUTH_REQUIRED','AUTH_EXPIRED','ACCESS_DENIED','RESOURCE_CHANGED','SOURCE_OFFLINE','PLUGIN_DISABLED'))
		)))
		ORDER BY c.account_id,c.external_id LIMIT max(0,1024-(SELECT count(*) FROM remote_sync_jobs WHERE state IN ('queued','running')))`, func(rows *sql.Rows) error {
		b := playlists.Binding{RemoteRef: playlists.RemoteRef{InstanceID: instance}, Platform: platform, ConfigRevision: revision}
		if err := rows.Scan(&b.AccountID, &b.ExternalID); err != nil {
			return err
		}
		bindings = append(bindings, b)
		return nil
	}, owner, instance, owner, instance, platform, account, account, account)
	if err != nil {
		return err
	}
	for _, b := range bindings {
		if _, _, err := schedulePlaylistCatalog(ctx, tx, owner, b); err != nil {
			return err
		}
	}
	return tx.Commit()
}
