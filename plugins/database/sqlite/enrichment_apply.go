package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func (s *Store) ApplyEnrichment(ctx context.Context, owner, candidateID, expectedObjectID string, expected int64, key string, guard mediastore.Guard) (mediastore.Selection, error) {
	return s.applyEnrichment(ctx, owner, candidateID, expectedObjectID, expected, key, guard, false, false)
}

func (s *Store) ApplyAutomaticEnrichment(ctx context.Context, owner, candidateID, expectedObjectID string, expected int64, key string, refreshMetadata bool, guard mediastore.Guard) (mediastore.Selection, error) {
	return s.applyEnrichment(ctx, owner, candidateID, expectedObjectID, expected, key, guard, true, refreshMetadata)
}

func (s *Store) applyEnrichment(ctx context.Context, owner, candidateID, expectedObjectID string, expected int64, key string, guard mediastore.Guard, automatic, refreshMetadata bool) (mediastore.Selection, error) {
	var empty mediastore.Selection
	if guard == nil {
		return empty, domain.Fail("INVALID_ARGUMENT", "Metadata selection requires a file guard")
	}
	// The immutable descriptor identifies the source before opening the mutation;
	// validity is checked only after durable successful-operation replay.
	c, err := s.EnrichmentCandidate(ctx, owner, candidateID)
	if err != nil {
		return empty, err
	}
	request := struct {
		CandidateID, ObjectID string
		ExpectedRevision      int64
		Automatic             bool
		RefreshMetadata       bool
	}{candidateID, expectedObjectID, expected, automatic, refreshMetadata}
	m, replay, err := s.beginContentMutation(ctx, owner, c.Target.SourceTrackID, "metadata-apply", key, expected, request)
	if err != nil {
		return empty, err
	}
	defer m.tx.Rollback()
	if replay {
		return m.result, nil
	}
	if automatic {
		var cancelled bool
		err = m.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM enrichment_candidates c
 JOIN metadata_automation a ON a.job_id=c.job_id AND a.owner=c.owner
 JOIN refresh_task_cancellations r ON r.job_id=a.scan_job_id WHERE c.owner=? AND c.id=?)`, owner, candidateID).Scan(&cancelled)
		if err != nil {
			return empty, err
		}
		if cancelled {
			return empty, domain.Fail("CANCELED", "Metadata refresh was cancelled")
		}
	}
	c, err = readEnrichmentCandidate(ctx, m.tx, owner, candidateID)
	if err != nil {
		return empty, err
	}
	if c.Status != "available" || c.PreviewObjectID == nil || *c.PreviewObjectID != expectedObjectID {
		return empty, domain.Fail("CONFLICT", "Metadata candidate does not match this selection revision")
	}
	if automatic && !enrichment.TrackSpecificCandidate(c) {
		return empty, domain.Fail("UNSUPPORTED", "Automatic metadata requires a local track-specific candidate")
	}
	var jobState, targetState, expires string
	var candidateGeneration, targetGeneration int64
	err = m.tx.QueryRowContext(ctx, `SELECT j.state,t.state,c.generation,t.generation,c.expires_at FROM enrichment_candidates c JOIN enrichment_jobs j ON j.id=c.job_id AND j.owner=c.owner JOIN enrichment_targets t ON t.job_id=c.job_id AND t.source_id=c.source_id AND t.owner=c.owner WHERE c.owner=? AND c.id=?`, owner, candidateID).Scan(&jobState, &targetState, &candidateGeneration, &targetGeneration, &expires)
	if err != nil {
		return empty, err
	}
	until, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || !time.Now().Before(until) || jobState == "cancelled" || jobState == "failed" || targetState != "completed" || candidateGeneration != targetGeneration {
		return empty, domain.Fail("CONFLICT", "Metadata candidate is expired or no longer active")
	}
	target := enrichment.ContentTarget(c.Target)
	if err = contentCurrentTarget(ctx, m.tx, owner, target); err != nil {
		return empty, err
	}
	object, err := readContentObject(ctx, m.tx, owner, expectedObjectID)
	if err != nil {
		return empty, err
	}
	if object.State != "available" || object.Metadata.Target != target || object.Metadata.Field != c.Field || object.Metadata.Language != c.Language || object.SHA256 != c.SHA256 || object.Size != c.SizeBytes {
		return empty, domain.Fail("RESOURCE_CHANGED", "Metadata preview object changed")
	}
	previous := ""
	old, locked := contentSelectedSlot(m.result, c.Field, c.Language)
	if automatic && (locked || (!refreshMetadata && old != nil && old.Current && old.State == "available")) {
		return empty, domain.Fail("CONFLICT", "Automatic content cannot replace an existing or manually locked selection")
	}
	unchanged := false
	if old != nil {
		previous = old.ID
		if automatic && c.Field == "artwork" {
			var recordingLocked bool
			err = m.tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM recording_cover_preferences p
 JOIN recording_source_links l ON l.owner=p.owner AND l.recording_id=p.recording_id
 WHERE p.owner=? AND p.object_id=? AND l.source_id=?)`, owner, old.ObjectID, target.SourceTrackID).Scan(&recordingLocked)
			if err != nil {
				return empty, err
			}
			if recordingLocked {
				return empty, domain.Fail("CONFLICT", "Automatic content cannot replace a manually selected recording cover")
			}
		}
		if refreshMetadata && old.Current && old.State == "available" {
			selected, readErr := readContentObject(ctx, m.tx, owner, old.ObjectID)
			if readErr != nil {
				return empty, readErr
			}
			unchanged = selected.SHA256 == object.SHA256 && selected.Size == object.Size && selected.MIME == object.MIME && selected.Metadata.Format == object.Metadata.Format
		}
	}
	var result mediastore.Selection
	var publicationErr error
	called := false
	err = guard(func() error {
		if called {
			return domain.Fail("CONFLICT", "Metadata candidate was already applied")
		}
		called = true
		if unchanged {
			result, publicationErr = m.finishUnchangedEnrichment(ctx)
		} else {
			result, publicationErr = m.selectObject(ctx, mediastore.SelectRequest{Target: target, ExpectedRevision: expected, ObjectID: expectedObjectID, Automatic: automatic}, object, previous)
		}
		return publicationErr
	})
	if err != nil {
		return empty, err
	}
	if !called {
		return empty, domain.Fail("CONFLICT", "Metadata selection guard did not publish")
	}
	return result, publicationErr
}

// 内容未改变时保留绑定和修订号，仍保存幂等结果并执行发布保护。
func (m *contentMutation) finishUnchangedEnrichment(ctx context.Context) (mediastore.Selection, error) {
	raw, err := json.Marshal(m.result)
	if err != nil {
		return mediastore.Selection{}, err
	}
	if err = saveContentIdempotency(ctx, m.tx, m.owner, m.route, m.key, m.digest, raw); err != nil {
		return mediastore.Selection{}, err
	}
	return m.result, m.tx.Commit()
}
