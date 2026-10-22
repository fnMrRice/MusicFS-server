package sqlite

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

// SetDefaultSource 将默认版本作为独立的本人录音偏好持久化，不改动成员与资产。
func (s *Store) SetDefaultSource(ctx context.Context, owner string, request matching.DefaultSourceRequest, key string) (matching.Mutation, error) {
	if request.RecordingID == "" || len(request.RecordingID) > 128 {
		return matching.Mutation{}, domain.Fail("INVALID_ARGUMENT", "A recording ID is required")
	}
	if request.SourceTrackID != nil {
		if _, err := sourceIDs([]string{*request.SourceTrackID}); err != nil {
			return matching.Mutation{}, err
		}
	}
	m, replayed, err := s.beginMatching(ctx, owner, "default-source", key, request.ExpectedRevision, request)
	if err != nil {
		return matching.Mutation{}, err
	}
	defer m.tx.Rollback()
	if replayed {
		return m.result, nil
	}
	recording, err := readRecording(ctx, m.tx, owner, request.RecordingID)
	if err != nil {
		return m.result, err
	}
	if request.SourceTrackID == nil {
		_, err = m.tx.ExecContext(ctx, "DELETE FROM recording_default_sources WHERE owner=? AND recording_id=?", owner, recording.ID)
	} else {
		if err = visibleSources(ctx, m.tx, owner, []string{*request.SourceTrackID}); err != nil {
			return m.result, err
		}
		member, err := linkRecording(ctx, m.tx, owner, *request.SourceTrackID)
		if err != nil {
			return m.result, err
		}
		if member != recording.ID {
			return m.result, domain.Fail("INVALID_ARGUMENT", "Default source is outside the recording")
		}
		_, err = m.tx.ExecContext(ctx, `INSERT INTO recording_default_sources(owner,recording_id,source_id) VALUES(?,?,?)
 ON CONFLICT(owner,recording_id) DO UPDATE SET source_id=excluded.source_id`, owner, recording.ID, *request.SourceTrackID)
		if err != nil {
			return m.result, err
		}
	}
	if err != nil {
		return m.result, err
	}
	if _, err = m.tx.ExecContext(ctx, "UPDATE recordings SET updated_at=? WHERE owner=? AND id=?", time.Now().UTC().Format(time.RFC3339Nano), owner, recording.ID); err != nil {
		return m.result, err
	}
	m.result.RecordingID = &recording.ID
	m.result.Changes = append(m.result.Changes, matching.LinkChange{Operation: "defaultSource", SourceTrackID: request.SourceTrackID, FromRecordingID: &recording.ID, ToRecordingID: &recording.ID})
	return m.finish(ctx)
}
