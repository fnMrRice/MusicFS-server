package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/processedcache"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
)

func (s *Store) ListProcessedCache(ctx context.Context) ([]processedcache.Record, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT document FROM processed_cache ORDER BY last_used_at DESC LIMIT ?", processedcache.MaxEntries+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []processedcache.Record{}
	for rows.Next() {
		var raw []byte
		var r processedcache.Record
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &r); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func authorizeProcessedCache(ctx context.Context, tx *sql.Tx, r processedcache.Record, a processedcache.Authorization) error {
	if err := r.Validate(); err != nil {
		return domain.Fail("INVALID_ARGUMENT", "Invalid completed processing output")
	}
	if r.Identity.Owner != a.Owner || a.AuthID == "" || a.SessionID == "" || a.AttemptID == "" {
		return domain.Fail("ACCESS_DENIED", "Processing cache scope differs")
	}
	var expiry string
	if err := tx.QueryRowContext(ctx, "SELECT expires_at FROM auth_sessions WHERE id=? AND user_id=?", a.AuthID, a.Owner).Scan(&expiry); err != nil {
		return domain.Fail("AUTH_REQUIRED", "Authentication session is unavailable")
	}
	until, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !time.Now().Before(until) {
		return domain.Fail("AUTH_REQUIRED", "Authentication session expired")
	}
	i := r.Identity
	t, err := timelineTarget(ctx, tx, a.Owner, timeline.Target{SourceTrackID: i.SourceTrackID, AssetID: i.AssetID, Version: i.Version})
	if err != nil {
		return err
	}
	if t.ProviderID != i.ProviderID {
		return domain.Fail("RESOURCE_CHANGED", "Processing provider identity changed")
	}
	var raw []byte
	if err = tx.QueryRowContext(ctx, "SELECT description,expires_at FROM playback_sessions WHERE id=? AND auth_session=? AND user_id=? AND state='active'", a.SessionID, a.AuthID, a.Owner).Scan(&raw, &expiry); err != nil {
		return domain.Fail("SESSION_EXPIRED", "Playback session is unavailable")
	}
	until, err = time.Parse(time.RFC3339Nano, expiry)
	var d struct {
		AttemptID        string `json:"attemptId"`
		AssetID          string `json:"assetId"`
		DeliveryMode     string `json:"deliveryMode"`
		PositionOriginMs int64  `json:"positionOriginMs"`
	}
	if err != nil || !time.Now().Before(until) || json.Unmarshal(raw, &d) != nil || d.AttemptID != a.AttemptID || d.AssetID != i.AssetID || d.DeliveryMode != "processed" || d.PositionOriginMs != i.PositionMs {
		return domain.Fail("CONFLICT", "Processing cache attempt changed")
	}
	return nil
}

func (s *Store) PublishProcessedCache(ctx context.Context, r processedcache.Record, a processedcache.Authorization) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeProcessedCache(ctx, tx, r, a); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM processed_cache").Scan(&count); err != nil {
		return err
	}
	if count >= processedcache.MaxEntries {
		return domain.Fail("RATE_LIMITED", "Completed processing cache limit reached")
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO processed_cache VALUES(?,?,?,?,?,?,?,?,?,?)", r.ID, r.Key, a.Owner, r.Identity.AssetID, r.Identity.Version, r.Size, r.SHA256, r.CreatedAt.UTC().Format(time.RFC3339Nano), r.LastUsedAt.UTC().Format(time.RFC3339Nano), raw)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) CheckProcessedCache(ctx context.Context, r processedcache.Record, a processedcache.Authorization) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = authorizeProcessedCache(ctx, tx, r, a); err != nil {
		return err
	}
	r.LastUsedAt = time.Now().UTC()
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE processed_cache SET last_used_at=?,document=? WHERE id=? AND cache_key=? AND owner=? AND asset_id=? AND asset_version=? AND size_bytes=? AND sha256=?", r.LastUsedAt.Format(time.RFC3339Nano), raw, r.ID, r.Key, a.Owner, r.Identity.AssetID, r.Identity.Version, r.Size, r.SHA256)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return domain.Fail("NOT_FOUND", "Completed processing cache is unavailable")
	}
	return tx.Commit()
}

func (s *Store) ForgetProcessedCache(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM processed_cache WHERE id=?", id)
	return err
}

var _ processedcache.Repository = (*Store)(nil)
