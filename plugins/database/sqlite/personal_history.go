package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func pruneRecent(ctx context.Context, tx *sql.Tx, owner string) error {
	now := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, "DELETE FROM personal_playback_receipts WHERE owner=? AND julianday(expires_at)<=julianday(?)", owner, now.Format(time.RFC3339Nano)); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM music_playlist_entries WHERE owner=? AND playlist_id='recently-played' AND binding_id='' AND (julianday(updated_at)<=julianday(?) OR entry_id IN (SELECT entry_id FROM music_playlist_entries WHERE owner=? AND playlist_id='recently-played' AND binding_id='' ORDER BY updated_at DESC,entry_id LIMIT -1 OFFSET ?))", owner, now.Add(-personal.HistoryRetention).Format(time.RFC3339Nano), owner, personal.MaxRecent)
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
	if description.SourceTrackID != input.SourceTrackID || description.AssetID != entry.Track.AssetID {
		return entry, domain.Fail("CONFLICT", "Playback session does not match the current source track")
	}
	if err = pruneRecent(ctx, tx, who.User.ID); err != nil {
		return entry, err
	}
	entry.ForcedSourceID = &input.SourceTrackID
	trackID, err := personalTrackID(ctx, tx, who.User.ID, input.SourceTrackID)
	if err != nil {
		return entry, err
	}
	entry.MusicTrack, err = readMusicTrack(ctx, tx, who.User.ID, trackID)
	if err != nil {
		return entry, err
	}
	var targetJSON *string
	var source, stamp string
	err = tx.QueryRowContext(ctx, "SELECT source_track_id,recorded_at,history_target FROM personal_playback_receipts WHERE owner=? AND playback_session_id=?", who.User.ID, input.PlaybackSessionID).Scan(&source, &stamp, &targetJSON)
	if err == nil {
		if source != input.SourceTrackID {
			return entry, domain.Fail("CONFLICT", "Playback session was already recorded for another source")
		}
		if targetJSON != nil {
			if err = json.Unmarshal([]byte(*targetJSON), &entry.HistoryTarget); err != nil {
				return entry, err
			}
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
	stamp = entry.RecordedAt.Format("2006-01-02T15:04:05.000000000Z")
	entry.HistoryTarget, err = separateHistoryTarget(ctx, tx, who.User.ID, input.SourceTrackID, input.SeparateTypes)
	if err != nil {
		return entry, err
	}
	if entry.HistoryTarget != nil {
		raw, err := json.Marshal(entry.HistoryTarget)
		if err != nil {
			return entry, err
		}
		encoded := string(raw)
		targetJSON = &encoded
		if err = queueContentHistory(ctx, tx, who.User.ID, input.PlaybackSessionID, *entry.HistoryTarget, stamp); err != nil {
			return entry, err
		}
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,forced_source_id,position,created_at,updated_at,play_count)
 VALUES(?,'recently-played','',?,?,?,0,?,?,1) ON CONFLICT(owner,playlist_id,binding_id,entry_id) DO UPDATE SET track_id=excluded.track_id,updated_at=excluded.updated_at,play_count=music_playlist_entries.play_count+1`, who.User.ID, input.SourceTrackID, trackID, input.SourceTrackID, stamp, stamp)
		if err != nil {
			return entry, err
		}
		if err = bumpPersonalRevision(ctx, tx, who.User.ID, personal.Recent); err != nil {
			return entry, err
		}
		if err = pruneRecent(ctx, tx, who.User.ID); err != nil {
			return entry, err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO personal_playback_receipts(owner,playback_session_id,source_track_id,recorded_at,expires_at,history_target) VALUES(?,?,?,?,?,?)", who.User.ID, input.PlaybackSessionID, input.SourceTrackID, stamp, expiresAt.UTC().Format(time.RFC3339Nano), targetJSON)
	if err != nil {
		return entry, err
	}
	return entry, tx.Commit()
}
