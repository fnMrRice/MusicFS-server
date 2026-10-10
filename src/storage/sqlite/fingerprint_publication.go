package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
)

// ReferencePublication is the only successful fingerprint completion boundary.
// The caller must retain the source read guard for this complete transaction.
func (s *Store) ReferencePublication(ctx context.Context, owner, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := readPublication(tx.QueryRowContext(ctx, "SELECT document FROM media_publications WHERE id=? AND owner=?", id, owner))
	if err != nil {
		return err
	}
	if p.Status == "referenced" || p.Status == "finished" {
		return nil
	}
	if p.Status != "published" {
		return domain.Fail("CONFLICT", "Media bytes are not published")
	}
	j, err := publicationJob(ctx, tx, p)
	if err != nil {
		return err
	}
	summary, err := publicationSummary(p, j)
	if err != nil {
		return err
	}
	if err = p.Object.Validate(); err != nil {
		return err
	}
	digest, err := j.FingerprintTarget.Digest()
	if err != nil {
		return err
	}
	i := identification.FingerprintIndex{ID: domain.NewID(), FingerprintTarget: j.FingerprintTarget, Object: p.Object, ActualStartSample: summary.ActualStartSample, SampleCount: summary.SampleCount, ValueCount: summary.ValueCount, Quality: summary.Quality, ComputedAt: summary.ComputedAt, Current: true}
	raw, err := json.Marshal(fingerprintIndexRecord{Index: i, FileSizeBytes: j.FileSizeBytes})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO fingerprint_indexes(id,owner,asset_id,job_id,publication_id,object_id,request_digest,current,document,computed_at) VALUES(?,?,?,?,?,?,?,1,?,?)", i.ID, owner, j.AssetID, j.ID, p.ID, p.Object.ID, digest, raw, i.ComputedAt.Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	j.Status, j.Error, j.ResultID, j.Result = "succeeded", nil, i.ID, &i
	if err = saveFingerprint(ctx, tx, &j, false); err != nil {
		return err
	}
	p.Status = "referenced"
	if err = savePublication(ctx, tx, &p, false); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) InvalidatePublication(ctx context.Context, owner, id, code string) error {
	if code != "RESOURCE_CHANGED" && code != "NOT_FOUND" {
		return identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	p, err := readPublication(tx.QueryRowContext(ctx, "SELECT document FROM media_publications WHERE id=? AND owner=?", id, owner))
	if err != nil {
		return err
	}
	if p.Status == "unavailable" {
		return nil
	}
	if p.Status != "referenced" && p.Status != "finished" {
		return domain.Fail("CONFLICT", "Only referenced media can become unavailable")
	}
	if _, err = tx.ExecContext(ctx, "UPDATE fingerprint_indexes SET current=0 WHERE owner=? AND publication_id=?", owner, id); err != nil {
		return err
	}
	// Completed reuse jobs may share this index; every public task view must
	// disclose the lost file instead of continuing to report successful evidence.
	rows, err := tx.QueryContext(ctx, "SELECT document FROM fingerprint_jobs WHERE owner=? AND result_id IN (SELECT id FROM fingerprint_indexes WHERE publication_id=? AND owner=?)", owner, id, owner)
	if err != nil {
		return err
	}
	var jobs []identification.FingerprintJob
	for rows.Next() {
		j, e := readFingerprint(rows)
		if e != nil {
			rows.Close()
			return e
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, j := range jobs {
		j.Status = "unavailable"
		j.Error = &domain.Error{Code: "RESOURCE_CHANGED", Message: "Fingerprint detail is missing or damaged"}
		if err = saveFingerprint(ctx, tx, &j, false); err != nil {
			return err
		}
	}
	p.Status = "unavailable"
	if err = savePublication(ctx, tx, &p, false); err != nil {
		return err
	}
	return tx.Commit()
}

var _ identification.FingerprintRepository = (*Store)(nil)

// Index and publication bindings are immutable. The owner check is repeated by
// InvalidatePublication, whose transaction updates every affected job and index.
func (s *Store) InvalidateFingerprint(ctx context.Context, owner, id string) error {
	var publication string
	err := s.DB.QueryRowContext(ctx, "SELECT i.publication_id FROM fingerprint_indexes i JOIN source_assets a ON a.id=i.asset_id JOIN tracks t ON t.id=a.track_id AND t.owner=i.owner WHERE i.id=? AND i.owner=?", id, owner).Scan(&publication)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Fingerprint index not found")
	}
	if err != nil {
		return err
	}
	return s.InvalidatePublication(ctx, owner, publication, "RESOURCE_CHANGED")
}
