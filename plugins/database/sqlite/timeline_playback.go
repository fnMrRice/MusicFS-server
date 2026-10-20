package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
)

func (s *Store) CommitRecovery(ctx context.Context, c timeline.Commit) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var expiry string
	err = tx.QueryRowContext(ctx, "SELECT expires_at FROM auth_sessions WHERE id=? AND user_id=?", c.AuthID, c.Owner).Scan(&expiry)
	until, parseErr := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || parseErr != nil || !time.Now().Before(until) {
		return domain.Fail("AUTH_REQUIRED", "Authentication session expired or was revoked")
	}
	if err = recoveryCurrent(ctx, tx, c); err != nil {
		return err
	}
	if _, err = timelineTarget(ctx, tx, c.Owner, c.Check.From); err != nil {
		return err
	}
	if _, err = timelineTarget(ctx, tx, c.Owner, c.Check.To); err != nil {
		return err
	}
	if c.Continue {
		position := c.Check.PositionMs
		if c.Check.From != c.Check.To {
			var evidence timeline.Evidence
			if evidence, err = checkTimeline(ctx, tx, c.Owner, c.Check); err != nil {
				return err
			}
			if position, err = evidence.Map(c.Check); err != nil {
				return err
			}
		}
		if err = verifyRecoveryDescription(c, position); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "UPDATE playback_sessions SET description=? WHERE id=? AND auth_session=? AND state='active'", c.Description, c.SessionID, c.AuthID)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO idempotency VALUES(?,?,?,?,?,?)", c.AuthID, c.Route, c.Key, c.Digest, c.Result, c.ExpiresAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

func verifyRecoveryDescription(c timeline.Commit, position int64) error {
	var output struct {
		SessionID        string    `json:"sessionId"`
		AttemptID        string    `json:"attemptId"`
		AssetID          string    `json:"assetId"`
		PositionOriginMs int64     `json:"positionOriginMs"`
		ExpiresAt        time.Time `json:"expiresAt"`
		DeliveryMode     string    `json:"deliveryMode"`
		SeekMode         string    `json:"seekMode"`
	}
	if json.Unmarshal(c.Description, &output) != nil || output.SessionID != c.SessionID || output.AttemptID == "" || output.AttemptID == c.PreviousAttemptID || output.AssetID != c.Check.To.AssetID || output.PositionOriginMs != position || !output.ExpiresAt.Equal(c.ExpiresAt) || output.DeliveryMode != "processed" || output.SeekMode != "timeRestart" {
		return domain.Fail("CONFLICT", "Recovery description differs from its validated mapping")
	}
	return nil
}
func recoveryCurrent(ctx context.Context, tx *sql.Tx, c timeline.Commit) error {
	var raw []byte
	var expiry string
	err := tx.QueryRowContext(ctx, "SELECT description,expires_at FROM playback_sessions WHERE id=? AND auth_session=? AND user_id=? AND state='active'", c.SessionID, c.AuthID, c.Owner).Scan(&raw, &expiry)
	if err != nil {
		return domain.Fail("SESSION_EXPIRED", "Playback session is no longer active")
	}
	until, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !time.Now().Before(until) || !until.Equal(c.ExpiresAt) {
		return domain.Fail("SESSION_EXPIRED", "Playback session expired")
	}
	var description struct {
		AttemptID string `json:"attemptId"`
		AssetID   string `json:"assetId"`
	}
	if err = json.Unmarshal(raw, &description); err != nil {
		return err
	}
	if description.AttemptID != c.PreviousAttemptID || description.AssetID != c.Check.From.AssetID {
		return domain.Fail("CONFLICT", "Playback recovery attempt changed")
	}
	return nil
}

var _ timeline.Repository = (*Store)(nil)
