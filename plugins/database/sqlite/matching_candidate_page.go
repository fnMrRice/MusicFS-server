package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func readMatchingCandidates(ctx context.Context, tx *sql.Tx, owner, source string, score int, after string, limit int) ([]candidateRecord, error) {
	rows, err := tx.QueryContext(ctx, `SELECT document FROM matching_candidates WHERE owner=? AND status='pending' AND (?='' OR left_source=? OR right_source=?) AND (score<? OR (score=? AND id>?)) ORDER BY score DESC,id LIMIT ?`, owner, source, source, source, score, score, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	records := []candidateRecord{}
	for rows.Next() {
		var raw []byte
		var record candidateRecord
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &record); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}
func currentCandidate(ctx context.Context, tx *sql.Tx, owner string, record candidateRecord, cache map[string]matching.CandidateSource) (bool, error) {
	for i, ref := range []candidates.AssetRef{record.Candidate.Left, record.Candidate.Right} {
		value, ok := cache[ref.SourceTrackID]
		if !ok {
			var err error
			value, err = matchingSource(ctx, tx, owner, ref.SourceTrackID)
			if err != nil {
				var failure *domain.Error
				if errors.As(err, &failure) && (failure.Code == "NOT_FOUND" || failure.Code == "RESOURCE_CHANGED") {
					return false, nil
				}
				return false, err
			}
			cache[ref.SourceTrackID] = value
		}
		binding := record.LeftBinding
		if i == 1 {
			binding = record.RightBinding
		}
		if value.Input.Asset != ref || value.BindingRevision != binding {
			return false, nil
		}
	}
	blocked, err := matchingBlocked(ctx, tx, owner, record.Candidate.Left.SourceTrackID, record.Candidate.Right.SourceTrackID)
	return !blocked, err
}
