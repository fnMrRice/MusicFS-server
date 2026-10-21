package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

// The binding covers every current index, even indexes beyond the bounded
// comparison selection. Publication and invalidation must invalidate snapshots.
func matchingSource(ctx context.Context, tx *sql.Tx, owner, id string) (matching.CandidateSource, error) {
	result := matching.CandidateSource{}
	var raw []byte
	var asset, version string
	var size int64
	err := tx.QueryRowContext(ctx, "SELECT document,asset_id,version,size FROM tracks WHERE owner=? AND id=?", owner, id).Scan(&raw, &asset, &version, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.Fail("NOT_FOUND", "Source track not found")
	} else if err != nil {
		return result, err
	}
	var track library.Track
	if err = json.Unmarshal(raw, &track); err != nil {
		return result, err
	}
	a, err := readAsset(ctx, tx, owner, asset)
	if err != nil {
		return result, err
	}
	if !a.Current || a.Version != version || a.SourceTrackID != id || track.Availability != "available" {
		return result, domain.Fail("RESOURCE_CHANGED", "Source evidence is no longer current")
	}
	h := sha256.New()
	fmt.Fprintf(h, "%d:", len(raw))
	h.Write(raw)
	assetRaw, err := json.Marshal(a)
	if err != nil {
		return result, err
	}
	fmt.Fprintf(h, "%d:", len(assetRaw))
	h.Write(assetRaw)
	result.Input = candidates.Input{Asset: candidates.AssetRef{SourceTrackID: id, AssetID: asset, AssetVersion: version}, Artists: append([]string{}, track.Artists...), DurationMs: track.DurationMs}
	if track.Title != nil {
		result.Input.Title = *track.Title
	}
	if a.HashStatus == "verified" && a.FileSHA512 != nil && a.FileSizeBytes != nil && *a.FileSizeBytes == size {
		for _, e := range a.Evidence {
			if e.Kind == "fileSha512" && e.Origin == "probe" && e.Status == "verified" && e.InvalidatedAt == nil && e.AssetID == asset && e.AssetVersion == version && e.SHA512 != nil && *e.SHA512 == *a.FileSHA512 {
				result.Input.FileHash = &candidates.VerifiedHash{Asset: result.Input.Asset, EvidenceID: e.ID, SHA512: *e.SHA512, FileSizeBytes: size}
			}
		}
	}
	rows, err := tx.QueryContext(ctx, "SELECT document,current FROM fingerprint_indexes WHERE owner=? AND asset_id=? AND current=1 ORDER BY id", owner, asset)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		index, err := readFingerprintIndex(rows)
		if err != nil {
			return result, err
		}
		raw, err := json.Marshal(index)
		if err != nil {
			return result, err
		}
		fmt.Fprintf(h, "%d:", len(raw))
		h.Write(raw)
		if index.Quality.Status == "available" && len(result.Fingerprints) < 8 {
			result.Fingerprints = append(result.Fingerprints, index)
		}
	}
	result.BindingRevision = hex.EncodeToString(h.Sum(nil))
	return result, rows.Err()
}

func (s *Store) CandidateSnapshot(ctx context.Context, owner, anchor string, ids []string, limit int) (matching.Snapshot, error) {
	result := matching.Snapshot{Owner: owner, Sources: []matching.CandidateSource{}, Blocked: []candidates.Pair{}}
	if limit < 1 || limit > matching.MaxSources || owner == "" {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid comparison pool")
	}
	if len(ids) > 0 {
		var err error
		ids, err = sourceIDs(ids)
		if err != nil {
			return result, err
		}
		for _, id := range ids {
			if id == anchor {
				return result, domain.Fail("INVALID_ARGUMENT", "Comparison pool must exclude its anchor")
			}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if result.Revision, err = matchingRevision(ctx, tx, owner); err != nil {
		return result, err
	}
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&result.LibraryRevision); err != nil {
		return result, err
	}
	if result.Anchor, err = matchingSource(ctx, tx, owner, anchor); err != nil {
		return result, err
	}
	if len(ids) == 0 {
		ids, err = matchingPool(ctx, tx, owner, result.Anchor, limit)
		if err != nil {
			return result, err
		}
	}
	for _, id := range ids {
		source, err := matchingSource(ctx, tx, owner, id)
		if err != nil {
			return result, err
		}
		result.Sources = append(result.Sources, source)
		blocked, err := matchingBlocked(ctx, tx, owner, anchor, id)
		if err != nil {
			return result, err
		}
		if blocked {
			result.Blocked = append(result.Blocked, candidates.Pair{LeftSourceTrackID: anchor, RightSourceTrackID: id})
		}
	}
	return result, nil
}

func matchingPool(ctx context.Context, tx *sql.Tx, owner string, anchor matching.CandidateSource, limit int) ([]string, error) {
	var hash []byte
	var size *int64
	if anchor.Input.FileHash != nil {
		hash, _ = hex.DecodeString(anchor.Input.FileHash.SHA512)
		size = &anchor.Input.FileHash.FileSizeBytes
	}
	artists, err := json.Marshal(anchor.Input.Artists)
	if err != nil {
		return nil, err
	}
	// This is one bounded anchor-to-pool query, never an all-pairs comparison.
	rows, err := tx.QueryContext(ctx, `SELECT t.id FROM tracks t JOIN source_assets a ON a.id=t.asset_id AND a.owner=t.owner
 WHERE t.owner=? AND t.id!=? AND a.current=1 AND json_extract(t.document,'$.availability')='available'
 AND NOT EXISTS(SELECT 1 FROM match_exclusions e WHERE e.owner=t.owner AND e.excluded=1 AND ((e.left_source=t.id AND e.right_source=?) OR (e.right_source=t.id AND e.left_source=?)))
 AND NOT (EXISTS(SELECT 1 FROM recording_source_links l WHERE l.owner=t.owner AND l.source_id=? AND l.recording_id IS NOT NULL) AND EXISTS(SELECT 1 FROM recording_source_links r WHERE r.owner=t.owner AND r.source_id=t.id AND r.recording_id IS NOT NULL))
 ORDER BY CASE WHEN a.file_sha512=? AND t.size=? THEN 0 ELSE 1 END,
 CASE WHEN lower(trim(json_extract(t.document,'$.title')))=lower(trim(?)) AND abs(json_extract(t.document,'$.durationMs')-?)<=2000
 AND EXISTS(SELECT 1 FROM json_each(t.document,'$.artists') x JOIN json_each(?) y ON lower(trim(x.value))=lower(trim(y.value)) WHERE trim(x.value)!='') THEN 0 ELSE 1 END,t.id LIMIT ?`, owner, anchor.Input.Asset.SourceTrackID, anchor.Input.Asset.SourceTrackID, anchor.Input.Asset.SourceTrackID, anchor.Input.Asset.SourceTrackID, hash, size, anchor.Input.Title, anchor.Input.DurationMs, string(artists), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func matchingBlocked(ctx context.Context, tx *sql.Tx, owner, left, right string) (bool, error) {
	if left > right {
		left, right = right, left
	}
	var excluded bool
	err := tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT excluded FROM match_exclusions WHERE owner=? AND left_source=? AND right_source=?),0)", owner, left, right).Scan(&excluded)
	if err != nil || excluded {
		return excluded, err
	}
	l, err := linkRecording(ctx, tx, owner, left)
	if err != nil {
		return false, err
	}
	r, err := linkRecording(ctx, tx, owner, right)
	// Two explicit assignments already decide this pair, whether together or apart.
	return l != "" && r != "", err
}
