package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/personal"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func pruneRecent(ctx context.Context, tx *sql.Tx, owner string) error {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, "DELETE FROM personal_playback_receipts WHERE owner=? AND julianday(expires_at)<=julianday(?)", owner, now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM personal_recently_played WHERE owner=? AND (julianday(played_at)<=julianday(?) OR source_track_id IN (SELECT source_track_id FROM personal_recently_played WHERE owner=? ORDER BY played_at DESC,source_track_id LIMIT -1 OFFSET ?))", owner, now.Add(-personal.HistoryRetention).Format(time.RFC3339Nano), owner, personal.MaxRecent)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		return bumpPersonalRevision(ctx, tx, owner, personal.Recent)
	}
	return nil
}

func (s *Store) RecordPlaybackStarted(ctx context.Context, who auth.Session, input personal.PlaybackStarted) (personal.Entry, error) {
	var entry personal.Entry
	if !personal.ValidID(input.SourceTrackID) || !personal.ValidID(input.PlaybackSessionID) || !personal.ValidID(input.AttemptID) {
		return entry, personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return entry, err
	}
	defer tx.Rollback()
	var raw []byte
	var state, expiry string
	err = tx.QueryRowContext(ctx, "SELECT description,state,expires_at FROM playback_sessions WHERE id=? AND auth_session=? AND user_id=?", input.PlaybackSessionID, who.ID, who.User.ID).Scan(&raw, &state, &expiry)
	if errors.Is(err, sql.ErrNoRows) {
		return entry, domain.Fail("NOT_FOUND", "Playback session was not found")
	}
	if err != nil {
		return entry, err
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil {
		return entry, err
	}
	if state != "active" || !time.Now().Before(expiresAt) {
		return entry, domain.Fail("SESSION_EXPIRED", "Playback session expired")
	}
	var description playback.Description
	if err = json.Unmarshal(raw, &description); err != nil {
		return entry, err
	}
	if description.AttemptID != input.AttemptID {
		return entry, domain.Fail("CONFLICT", "Playback attempt was superseded")
	}
	entry.Track, err = readPersonalTrack(ctx, tx, who.User.ID, input.SourceTrackID)
	if err != nil {
		return entry, err
	}
	if description.AssetID != entry.Track.AssetID {
		return entry, domain.Fail("CONFLICT", "Playback session does not match the current source track")
	}
	if err = pruneRecent(ctx, tx, who.User.ID); err != nil {
		return entry, err
	}
	var source, stamp string
	err = tx.QueryRowContext(ctx, "SELECT source_track_id,recorded_at FROM personal_playback_receipts WHERE owner=? AND playback_session_id=?", who.User.ID, input.PlaybackSessionID).Scan(&source, &stamp)
	if err == nil {
		if source != input.SourceTrackID {
			return entry, domain.Fail("CONFLICT", "Playback session was already recorded for another source")
		}
		entry.RecordedAt, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return entry, err
		}
		return entry, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return entry, err
	}
	entry.RecordedAt = time.Now().UTC()
	stamp = entry.RecordedAt.Format(time.RFC3339Nano)
	if _, err = tx.ExecContext(ctx, "INSERT INTO personal_recently_played VALUES(?,?,?) ON CONFLICT(owner,source_track_id) DO UPDATE SET played_at=excluded.played_at", who.User.ID, input.SourceTrackID, stamp); err != nil {
		return entry, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO personal_playback_receipts VALUES(?,?,?,?,?)", who.User.ID, input.PlaybackSessionID, input.SourceTrackID, stamp, expiresAt.UTC().Format(time.RFC3339Nano)); err != nil {
		return entry, err
	}
	if err = bumpPersonalRevision(ctx, tx, who.User.ID, personal.Recent); err != nil {
		return entry, err
	}
	if err = pruneRecent(ctx, tx, who.User.ID); err != nil {
		return entry, err
	}
	return entry, tx.Commit()
}
