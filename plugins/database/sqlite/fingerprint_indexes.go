package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

type fingerprintIndexRecord struct {
	Index         identification.FingerprintIndex
	FileSizeBytes int64
}

func readFingerprintIndex(row rowScanner) (identification.FingerprintIndex, error) {
	var raw []byte
	var r fingerprintIndexRecord
	var current bool
	if err := row.Scan(&raw, &current); err != nil {
		return r.Index, err
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return r.Index, err
	}
	i := r.Index
	i.Current = current
	i.FileSizeBytes = r.FileSizeBytes
	i.Object.RelativePath = "objects/" + i.Object.ID
	if err := i.Object.Validate(); err != nil {
		return i, err
	}
	return i, nil
}

func hydrateFingerprint(ctx context.Context, tx *sql.Tx, j *identification.FingerprintJob) error {
	if j.ResultID == "" {
		j.Result = nil
		return nil
	}
	index, err := readFingerprintIndex(tx.QueryRowContext(ctx, "SELECT i.document,i.current FROM fingerprint_indexes i JOIN source_assets a ON a.id=i.asset_id JOIN tracks t ON t.id=a.track_id AND t.owner=i.owner WHERE i.owner=? AND i.id=?", j.Owner, j.ResultID))
	if err == nil {
		j.Result = &index
	}
	return err
}

func (s *Store) FingerprintIndex(ctx context.Context, owner, id string) (identification.FingerprintIndex, error) {
	i, err := readFingerprintIndex(s.DB.QueryRowContext(ctx, "SELECT i.document,i.current FROM fingerprint_indexes i JOIN source_assets a ON a.id=i.asset_id JOIN tracks t ON t.id=a.track_id AND t.owner=i.owner WHERE i.owner=? AND i.id=?", owner, id))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Fingerprint index not found")
	}
	return i, err
}

func (s *Store) FingerprintIndexes(ctx context.Context, owner, track, after string, limit int) ([]identification.FingerprintIndex, error) {
	if limit < 1 || limit > 200 {
		return nil, identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var asset string
	if err = tx.QueryRowContext(ctx, "SELECT asset_id FROM tracks WHERE id=? AND owner=?", track, owner).Scan(&asset); errors.Is(err, sql.ErrNoRows) {
		return nil, domain.Fail("NOT_FOUND", "Source track not found")
	} else if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document,current FROM fingerprint_indexes WHERE owner=? AND asset_id=? AND current=1 AND id>? ORDER BY id LIMIT ?", owner, asset, after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []identification.FingerprintIndex{}
	for rows.Next() {
		i, err := readFingerprintIndex(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	return items, rows.Err()
}

func (s *Store) FingerprintCandidates(ctx context.Context, scope identification.HashScope, after string, request identification.FingerprintRequest, limit int) ([]identification.HashTarget, error) {
	if limit < 1 || limit > 64 {
		return nil, identification.Invalid()
	}
	if err := request.Validate(); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT t.id,t.asset_id,t.version,t.size FROM tracks t JOIN source_assets a ON a.id=t.asset_id WHERE t.owner=? AND t.provider=? AND t.id>? AND a.current=1 AND json_extract(t.document,'$.availability')='available' AND NOT EXISTS(SELECT 1 FROM fingerprint_jobs j WHERE j.owner=t.owner AND j.asset_id=a.id AND j.config_revision=? AND j.request_start_ms=? AND j.request_duration_ms=?) ORDER BY t.id LIMIT ?", scope.Owner, scope.ProviderInstanceID, after, request.ExtractorIdentity.ConfigRevision, request.StartMs, request.DurationMs, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []identification.HashTarget{}
	for rows.Next() {
		var t identification.HashTarget
		if err = rows.Scan(&t.SourceTrackID, &t.AssetID, &t.AssetVersion, &t.FileSizeBytes); err != nil {
			return nil, err
		}
		items = append(items, t)
	}
	return items, rows.Err()
}
