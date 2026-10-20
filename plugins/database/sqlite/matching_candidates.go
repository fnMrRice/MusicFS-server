package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/matching"
)

type candidateRecord struct {
	Candidate                 matching.Candidate
	LeftBinding, RightBinding string
}

func (s *Store) SaveCandidates(ctx context.Context, owner string, snapshot matching.Snapshot, results []candidates.Candidate) (matching.CandidatePage, error) {
	page := matching.CandidatePage{Revision: snapshot.Revision, Items: []matching.Candidate{}}
	if owner == "" || owner != snapshot.Owner || len(snapshot.Sources) > matching.MaxSources || len(results) > candidates.MaxResults {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid candidate snapshot")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	current, err := validateMatchingSnapshot(ctx, tx, owner, snapshot)
	if err != nil {
		return page, err
	}
	anchor := snapshot.Anchor.Input.Asset.SourceTrackID
	seen := map[string]bool{}
	records := make([]candidateRecord, 0, len(results))
	digests := make([]string, 0, len(results))
	for _, result := range results {
		left, right := current[result.Left.SourceTrackID], current[result.Right.SourceTrackID]
		if err = validateCandidate(result, left, right, anchor); err != nil {
			return page, err
		}
		pair := result.Left.SourceTrackID + "/" + result.Right.SourceTrackID
		if seen[pair] {
			return page, domain.Fail("INVALID_ARGUMENT", "Repeated candidate pair")
		}
		seen[pair] = true
		blocked, e := matchingBlocked(ctx, tx, owner, result.Left.SourceTrackID, result.Right.SourceTrackID)
		if e != nil {
			return page, e
		}
		if blocked {
			return page, domain.Fail("CONFLICT", "Candidate conflicts with a manual decision")
		}
		r := candidateRecord{Candidate: matching.Candidate{ID: domain.NewID(), Candidate: result, Status: "pending", CreatedAt: time.Now().UTC()}, LeftBinding: left.BindingRevision, RightBinding: right.BindingRevision}
		binding, e := json.Marshal(struct {
			Result      candidates.Candidate
			Left, Right string
			Revision    int64
		}{result, r.LeftBinding, r.RightBinding, snapshot.Revision})
		if e != nil {
			return page, e
		}
		sum := sha256.Sum256(binding)
		digest := hex.EncodeToString(sum[:])
		var existing []byte
		e = tx.QueryRowContext(ctx, "SELECT document FROM matching_candidates WHERE owner=? AND left_source=? AND right_source=? AND status='pending' AND binding_digest=? ORDER BY id LIMIT 1", owner, result.Left.SourceTrackID, result.Right.SourceTrackID, digest).Scan(&existing)
		if e == nil {
			var old candidateRecord
			if e = json.Unmarshal(existing, &old); e != nil {
				return page, e
			}
			r.Candidate.ID, r.Candidate.CreatedAt = old.Candidate.ID, old.Candidate.CreatedAt
		} else if !errors.Is(e, sql.ErrNoRows) {
			return page, e
		}
		records, digests = append(records, r), append(digests, digest)
	}
	if _, err = tx.ExecContext(ctx, "UPDATE matching_candidates SET status='stale' WHERE owner=? AND status='pending' AND (left_source=? OR right_source=?)", owner, anchor, anchor); err != nil {
		return page, err
	}
	for i, record := range records {
		c := record.Candidate
		raw, err := json.Marshal(record)
		if err != nil {
			return page, err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO matching_candidates(id,owner,left_source,right_source,left_asset,right_asset,score,status,binding_digest,document,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(id) DO UPDATE SET status='pending'`, c.ID, owner, c.Left.SourceTrackID, c.Right.SourceTrackID, c.Left.AssetID, c.Right.AssetID, c.Score, c.Status, digests[i], raw, c.CreatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return page, err
		}
		page.Items = append(page.Items, c)
	}
	sort.Slice(page.Items, func(i, j int) bool {
		if page.Items[i].Score != page.Items[j].Score {
			return page.Items[i].Score > page.Items[j].Score
		}
		return page.Items[i].ID < page.Items[j].ID
	})
	return page, tx.Commit()
}

func validateMatchingSnapshot(ctx context.Context, tx *sql.Tx, owner string, snapshot matching.Snapshot) (map[string]matching.CandidateSource, error) {
	revision, err := matchingRevision(ctx, tx, owner)
	if err != nil {
		return nil, err
	}
	var libraryRevision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&libraryRevision); err != nil {
		return nil, err
	}
	if revision != snapshot.Revision || libraryRevision != snapshot.LibraryRevision {
		return nil, domain.Fail("CONFLICT", "Matching or library revision changed")
	}
	current := map[string]matching.CandidateSource{}
	sources := append([]matching.CandidateSource{snapshot.Anchor}, snapshot.Sources...)
	for _, source := range sources {
		id := source.Input.Asset.SourceTrackID
		if _, exists := current[id]; exists {
			return nil, domain.Fail("INVALID_ARGUMENT", "Repeated comparison source")
		}
		fresh, err := matchingSource(ctx, tx, owner, id)
		if err != nil {
			return nil, err
		}
		if fresh.Input.Asset != source.Input.Asset || fresh.BindingRevision != source.BindingRevision {
			return nil, domain.Fail("CONFLICT", "Candidate evidence changed")
		}
		current[id] = fresh
	}
	return current, nil
}

func (s *Store) Candidates(ctx context.Context, owner, source, after string, limit int) (matching.CandidatePage, error) {
	page := matching.CandidatePage{Items: []matching.Candidate{}}
	if limit < 1 || limit > 100 {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid candidate page")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = visibleSources(ctx, tx, owner, []string{source}); err != nil {
		return page, err
	}
	if page.Revision, err = matchingRevision(ctx, tx, owner); err != nil {
		return page, err
	}
	lastScore := 101
	if after != "" {
		err = tx.QueryRowContext(ctx, "SELECT score FROM matching_candidates WHERE owner=? AND id=? AND (left_source=? OR right_source=?)", owner, after, source, source).Scan(&lastScore)
		if errors.Is(err, sql.ErrNoRows) {
			return page, domain.Fail("CURSOR_EXPIRED", "Candidate cursor is no longer available")
		} else if err != nil {
			return page, err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT document FROM matching_candidates WHERE owner=? AND status='pending' AND (left_source=? OR right_source=?) AND (score<? OR (score=? AND id>?)) ORDER BY score DESC,id LIMIT ?`, owner, source, source, lastScore, lastScore, after, limit+1)
	if err != nil {
		return page, err
	}
	var records []candidateRecord
	for rows.Next() {
		var raw []byte
		var record candidateRecord
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return page, err
		}
		if err = json.Unmarshal(raw, &record); err != nil {
			rows.Close()
			return page, err
		}
		records = append(records, record)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(records) > limit {
		records = records[:limit]
		next := records[limit-1].Candidate.ID
		page.NextAfter = &next
	}
	cache := map[string]matching.CandidateSource{}
	for _, record := range records {
		fresh := true
		for i, ref := range []candidates.AssetRef{record.Candidate.Left, record.Candidate.Right} {
			value, ok := cache[ref.SourceTrackID]
			if !ok {
				value, err = matchingSource(ctx, tx, owner, ref.SourceTrackID)
				if err != nil {
					var failure *domain.Error
					if errors.As(err, &failure) && (failure.Code == "NOT_FOUND" || failure.Code == "RESOURCE_CHANGED") {
						fresh = false
						break
					}
					return page, err
				}
				cache[ref.SourceTrackID] = value
			}
			binding := record.LeftBinding
			if i == 1 {
				binding = record.RightBinding
			}
			if value.Input.Asset != ref || value.BindingRevision != binding {
				fresh = false
				break
			}
		}
		if !fresh {
			continue
		}
		blocked, err := matchingBlocked(ctx, tx, owner, record.Candidate.Left.SourceTrackID, record.Candidate.Right.SourceTrackID)
		if err != nil {
			return page, err
		}
		if !blocked {
			page.Items = append(page.Items, record.Candidate)
		}
	}
	return page, nil
}
