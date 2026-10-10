package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
)

func timelineTarget(ctx context.Context, tx *sql.Tx, owner string, target timeline.Target) (library.Track, error) {
	var track library.Track
	if err := target.Validate(); err != nil {
		return track, err
	}
	var raw []byte
	var asset, version string
	err := tx.QueryRowContext(ctx, "SELECT t.document,t.asset_id,t.version FROM tracks t JOIN source_assets a ON a.id=t.asset_id AND a.owner=t.owner AND a.track_id=t.id AND a.version=t.version AND a.current=1 WHERE t.owner=? AND t.id=?", owner, target.SourceTrackID).Scan(&raw, &asset, &version)
	if errors.Is(err, sql.ErrNoRows) {
		return track, domain.Fail("NOT_FOUND", "Current source asset not found")
	}
	if err != nil {
		return track, err
	}
	if asset != target.AssetID || version != target.Version {
		return track, domain.Fail("RESOURCE_CHANGED", "Timeline source asset changed")
	}
	if err = json.Unmarshal(raw, &track); err != nil {
		return track, err
	}
	if track.Availability != "available" {
		return track, domain.Fail("SOURCE_OFFLINE", "Timeline source is unavailable")
	}
	return track, nil
}
func timelinePair(ctx context.Context, tx *sql.Tx, owner string, from, to timeline.Target) (library.Track, library.Track, string, error) {
	a, err := timelineTarget(ctx, tx, owner, from)
	if err != nil {
		return a, library.Track{}, "", err
	}
	b, err := timelineTarget(ctx, tx, owner, to)
	if err != nil {
		return a, b, "", err
	}
	left, err := linkRecording(ctx, tx, owner, from.SourceTrackID)
	if err != nil {
		return a, b, "", err
	}
	right, err := linkRecording(ctx, tx, owner, to.SourceTrackID)
	if err != nil {
		return a, b, "", err
	}
	if left == "" || left != right {
		return a, b, "", domain.Fail("RESTART_REQUIRED", "Sources do not share a confirmed recording")
	}
	return a, b, left, nil
}
func readTimeline(ctx context.Context, tx *sql.Tx, owner, id string) (timeline.Evidence, error) {
	var result timeline.Evidence
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM timeline_evidence WHERE owner=? AND id=?", owner, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.Fail("NOT_FOUND", "Timeline evidence not found")
	}
	if err == nil {
		err = json.Unmarshal(raw, &result)
	}
	return result, err
}
func checkTimeline(ctx context.Context, tx *sql.Tx, owner string, c timeline.Check) (timeline.Evidence, error) {
	var result timeline.Evidence
	a, b, recording, err := timelinePair(ctx, tx, owner, c.From, c.To)
	if err != nil {
		return result, err
	}
	if c.EvidenceID == "" {
		return result, domain.Fail("RESTART_REQUIRED", "Independent timeline evidence is required")
	}
	result, err = readTimeline(ctx, tx, owner, c.EvidenceID)
	if err != nil {
		return result, err
	}
	if result.RecordingID != recording {
		return result, domain.Fail("RESTART_REQUIRED", "Timeline recording association changed")
	}
	position, err := result.Map(c)
	if err != nil {
		return result, err
	}
	if a.DurationMs == nil || b.DurationMs == nil || c.PositionMs >= *a.DurationMs || position >= *b.DurationMs || result.Range.EndMs > *a.DurationMs || result.Range.EndMs+result.OffsetMs > *b.DurationMs {
		return result, domain.Fail("RESTART_REQUIRED", "Timeline coverage exceeds the current verified duration")
	}
	return result, nil
}
func (s *Store) CheckTimeline(ctx context.Context, owner string, c timeline.Check) (timeline.Evidence, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return timeline.Evidence{}, err
	}
	defer tx.Rollback()
	return checkTimeline(ctx, tx, owner, c)
}
func (s *Store) Timeline(ctx context.Context, owner, id string) (timeline.Evidence, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return timeline.Evidence{}, err
	}
	defer tx.Rollback()
	return readTimeline(ctx, tx, owner, id)
}
