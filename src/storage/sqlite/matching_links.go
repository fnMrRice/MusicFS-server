package sqlite

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/matching"
)

func createRecording(ctx context.Context, tx *sql.Tx, owner string) (string, error) {
	id := domain.NewID()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := tx.ExecContext(ctx, "INSERT INTO recordings VALUES(?,?,?,?)", id, owner, now, now)
	return id, err
}

func (m *matchingMutation) moveSources(ctx context.Context, operation string, sources []string, target string) error {
	for _, source := range sources {
		old, err := linkRecording(ctx, m.tx, m.owner, source)
		if err != nil {
			return err
		}
		decision := "linked"
		if target == "" {
			decision = "unlinked"
		}
		_, err = m.tx.ExecContext(ctx, "INSERT INTO recording_source_links(owner,source_id,recording_id,decision,revision) VALUES(?,?,?,?,?) ON CONFLICT(owner,source_id) DO UPDATE SET recording_id=excluded.recording_id,decision=excluded.decision,revision=excluded.revision", m.owner, source, strptr(target), decision, m.result.Revision)
		if err != nil {
			return err
		}
		if _, err = m.tx.ExecContext(ctx, "UPDATE recordings SET updated_at=? WHERE owner=? AND id IN (?,?)", time.Now().UTC().Format(time.RFC3339Nano), m.owner, old, target); err != nil {
			return err
		}
		m.result.Changes = append(m.result.Changes, matching.LinkChange{Operation: operation, SourceTrackID: strptr(source), FromRecordingID: strptr(old), ToRecordingID: strptr(target)})
	}
	return refreshManualCandidateStates(ctx, m.tx, m.owner, sources)
}

func (s *Store) ManualLink(ctx context.Context, owner string, request matching.LinkRequest, key string) (matching.Mutation, error) {
	ids, err := sourceIDs(request.SourceTrackIDs)
	if err != nil {
		return matching.Mutation{}, err
	}
	request.SourceTrackIDs = ids
	if request.TargetRecordingID != nil && *request.TargetRecordingID == "" {
		return matching.Mutation{}, domain.Fail("INVALID_ARGUMENT", "Target recording must be null or a nonempty ID")
	}
	m, replayed, err := s.beginMatching(ctx, owner, "link", key, request.ExpectedRevision, request)
	if err != nil {
		return matching.Mutation{}, err
	}
	defer m.tx.Rollback()
	if replayed {
		return m.result, nil
	}
	if err = visibleSources(ctx, m.tx, owner, ids); err != nil {
		return m.result, err
	}
	target := ""
	members := []string{}
	if request.TargetRecordingID != nil {
		recording, err := readRecording(ctx, m.tx, owner, *request.TargetRecordingID)
		if err != nil {
			return m.result, err
		}
		target, members = recording.ID, recording.SourceTrackIDs
	}
	union := map[string]bool{}
	for _, id := range members {
		union[id] = true
	}
	for _, id := range ids {
		union[id] = true
	}
	if len(union) > matching.MaxSources {
		return m.result, domain.Fail("RATE_LIMITED", "Recording member limit exceeded")
	}
	all := make([]string, 0, len(union))
	for id := range union {
		all = append(all, id)
	}
	sort.Strings(all)
	if err = checkExclusions(ctx, m.tx, owner, all); err != nil {
		return m.result, err
	}
	if target == "" {
		target, err = createRecording(ctx, m.tx, owner)
		if err != nil {
			return m.result, err
		}
	}
	m.result.RecordingID = strptr(target)
	if err = m.moveSources(ctx, "link", ids, target); err != nil {
		return m.result, err
	}
	return m.finish(ctx)
}

func (s *Store) Unlink(ctx context.Context, owner string, request matching.UnlinkRequest, key string) (matching.Mutation, error) {
	ids, err := sourceIDs([]string{request.SourceTrackID})
	if err != nil {
		return matching.Mutation{}, err
	}
	m, replayed, err := s.beginMatching(ctx, owner, "unlink", key, request.ExpectedRevision, request)
	if err != nil {
		return matching.Mutation{}, err
	}
	defer m.tx.Rollback()
	if replayed {
		return m.result, nil
	}
	if err = visibleSources(ctx, m.tx, owner, ids); err != nil {
		return m.result, err
	}
	if err = m.moveSources(ctx, "unlink", ids, ""); err != nil {
		return m.result, err
	}
	return m.finish(ctx)
}

func (s *Store) Split(ctx context.Context, owner string, request matching.SplitRequest, key string) (matching.Mutation, error) {
	ids, err := sourceIDs(request.SourceTrackIDs)
	if err != nil {
		return matching.Mutation{}, err
	}
	request.SourceTrackIDs = ids
	m, replayed, err := s.beginMatching(ctx, owner, "split", key, request.ExpectedRevision, request)
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
	if err = visibleSources(ctx, m.tx, owner, ids); err != nil {
		return m.result, err
	}
	if len(ids) >= len(recording.SourceTrackIDs) {
		return m.result, domain.Fail("INVALID_ARGUMENT", "Split requires a nonempty proper subset")
	}
	members := map[string]bool{}
	for _, id := range recording.SourceTrackIDs {
		members[id] = true
	}
	for _, id := range ids {
		if !members[id] {
			return m.result, domain.Fail("INVALID_ARGUMENT", "Split source is outside the recording")
		}
	}
	if err = checkExclusions(ctx, m.tx, owner, ids); err != nil {
		return m.result, err
	}
	target, err := createRecording(ctx, m.tx, owner)
	if err != nil {
		return m.result, err
	}
	m.result.RecordingID = strptr(target)
	if err = m.moveSources(ctx, "split", ids, target); err != nil {
		return m.result, err
	}
	return m.finish(ctx)
}

func (s *Store) SetExclusion(ctx context.Context, owner string, request matching.ExclusionRequest, key string) (matching.Mutation, error) {
	ids, err := sourceIDs([]string{request.LeftSourceTrackID, request.RightSourceTrackID})
	if err != nil {
		return matching.Mutation{}, err
	}
	request.LeftSourceTrackID, request.RightSourceTrackID = ids[0], ids[1]
	m, replayed, err := s.beginMatching(ctx, owner, "exclusion", key, request.ExpectedRevision, request)
	if err != nil {
		return matching.Mutation{}, err
	}
	defer m.tx.Rollback()
	if replayed {
		return m.result, nil
	}
	if err = visibleSources(ctx, m.tx, owner, ids); err != nil {
		return m.result, err
	}
	left, err := linkRecording(ctx, m.tx, owner, ids[0])
	if err != nil {
		return m.result, err
	}
	right, err := linkRecording(ctx, m.tx, owner, ids[1])
	if err != nil {
		return m.result, err
	}
	if request.Excluded && left != "" && left == right {
		return m.result, domain.Fail("CONFLICT", "Unlink or split the recording before excluding its members")
	}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO match_exclusions VALUES(?,?,?,?,?) ON CONFLICT(owner,left_source,right_source) DO UPDATE SET excluded=excluded.excluded,revision=excluded.revision", owner, ids[0], ids[1], request.Excluded, m.result.Revision)
	if err != nil {
		return m.result, err
	}
	m.result.Changes = append(m.result.Changes, matching.LinkChange{Operation: "exclusion", LeftSourceTrackID: strptr(ids[0]), RightSourceTrackID: strptr(ids[1]), Excluded: &request.Excluded})
	if err = refreshManualCandidateStates(ctx, m.tx, owner, ids); err != nil {
		return m.result, err
	}
	return m.finish(ctx)
}
