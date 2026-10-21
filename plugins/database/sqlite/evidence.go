package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
)

func currentBinding(ctx context.Context, tx *sql.Tx, owner string, b identification.Binding) (identification.Asset, error) {
	a, err := readAsset(ctx, tx, owner, b.AssetID)
	if err != nil {
		return a, err
	}
	if !a.Current || a.Version != b.AssetVersion || a.SourceTrackID != b.SourceTrackID {
		return a, domain.Fail("CONFLICT", "Asset version is no longer current")
	}
	var count int
	err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM tracks WHERE owner=? AND id=? AND asset_id=? AND version=?", owner, b.SourceTrackID, b.AssetID, b.AssetVersion).Scan(&count)
	if err == nil && count != 1 {
		err = domain.Fail("CONFLICT", "Asset version is no longer current")
	}
	return a, err
}

func insertEvidence(ctx context.Context, tx *sql.Tx, owner string, a identification.Asset, spec identification.Spec, result identification.Result) (identification.Evidence, error) {
	if result.ComputedAt != nil {
		utc := result.ComputedAt.UTC()
		result.ComputedAt = &utc
	}
	e := identification.Evidence{ID: domain.NewID(), AssetID: a.ID, AssetVersion: a.Version, EvidenceVersion: identification.EvidenceVersion, Spec: spec, Result: result}
	if err := spec.Validate(a.FileSizeBytes); err != nil {
		return e, err
	}
	err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(generation),0)+1 FROM asset_evidence WHERE asset_id=? AND kind=? AND origin=?", a.ID, spec.Kind, spec.Origin).Scan(&e.Generation)
	if err != nil {
		return e, err
	}
	if err := invalidateEvidence(ctx, tx, owner, a.ID, spec.Kind, spec.Origin); err != nil {
		return e, err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	var digest []byte
	if e.SHA512 != nil {
		digest, err = hex.DecodeString(*e.SHA512)
		if err != nil {
			return e, err
		}
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO asset_evidence(id,owner,asset_id,kind,origin,generation,status,digest,document,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)", e.ID, owner, a.ID, e.Kind, e.Origin, e.Generation, e.Status, digest, raw, time.Now().UTC().Format(time.RFC3339Nano))
	return e, err
}

func (s *Store) BeginEvidence(ctx context.Context, owner string, b identification.Binding) (identification.Ticket, error) {
	var ticket identification.Ticket
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return ticket, err
	}
	defer tx.Rollback()
	a, err := currentBinding(ctx, tx, owner, b)
	if err != nil {
		return ticket, err
	}
	e, err := insertEvidence(ctx, tx, owner, a, b.Spec, identification.Result{Status: "pending", BitrateMode: "unknown"})
	if err != nil {
		return ticket, err
	}
	if err := publishAssetView(ctx, tx, owner, a); err != nil {
		return ticket, err
	}
	ticket = identification.Ticket{ID: e.ID, Generation: e.Generation, Binding: b}
	return ticket, tx.Commit()
}

func pendingEvidence(ctx context.Context, tx *sql.Tx, owner string, ticket identification.Ticket) (identification.Evidence, error) {
	var e identification.Evidence
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM asset_evidence WHERE id=? AND owner=?", ticket.ID, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return e, domain.Fail("NOT_FOUND", "Evidence task not found")
	}
	if err != nil {
		return e, err
	}
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, err
	}
	if e.Status != "pending" || e.InvalidatedAt != nil || e.Generation != ticket.Generation || e.AssetID != ticket.AssetID || e.AssetVersion != ticket.AssetVersion || !reflect.DeepEqual(e.Spec, ticket.Spec) {
		return e, domain.Fail("CONFLICT", "Evidence task generation is no longer active")
	}
	return e, nil
}

// PublishEvidence is a trusted-worker boundary. A verified digest is accepted
// only for a complete byte range, matching current asset and active generation.
// The hashing worker holds stable complete-read protection through its own
// atomic task-and-evidence publication transaction.
func (s *Store) PublishEvidence(ctx context.Context, owner string, ticket identification.Ticket, result identification.Result) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := publishEvidenceResult(ctx, tx, owner, ticket, result); err != nil {
		return err
	}
	return tx.Commit()
}

func publishEvidenceResult(ctx context.Context, tx *sql.Tx, owner string, ticket identification.Ticket, result identification.Result) error {
	a, err := currentBinding(ctx, tx, owner, ticket.Binding)
	if err != nil {
		return err
	}
	e, err := pendingEvidence(ctx, tx, owner, ticket)
	if err != nil {
		return err
	}
	if err := result.Validate(e.Spec); err != nil {
		return err
	}
	if result.ComputedAt != nil {
		utc := result.ComputedAt.UTC()
		result.ComputedAt = &utc
	}
	e.Result = result
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	var digest []byte
	if e.SHA512 != nil {
		digest, err = hex.DecodeString(*e.SHA512)
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE asset_evidence SET status=?,digest=?,document=? WHERE id=? AND owner=?", e.Status, digest, raw, e.ID, owner); err != nil {
		return err
	}
	if err := publishAssetView(ctx, tx, owner, a); err != nil {
		return err
	}
	return nil
}

func (s *Store) CancelEvidence(ctx context.Context, owner string, ticket identification.Ticket) error {
	code := "CANCELED"
	return s.PublishEvidence(ctx, owner, ticket, identification.Result{Status: "failed", BitrateMode: "unknown", FailureCode: &code})
}

type EvidenceAudit struct {
	Sequence int64
	Evidence identification.Evidence
}

// EvidenceHistory is an owner-scoped, bounded audit page, newest first. Continue
// by supplying the last item's Sequence as before; zero starts at the latest.
func (s *Store) EvidenceHistory(ctx context.Context, owner, asset string, before int64, limit int) ([]EvidenceAudit, error) {
	if before < 0 || limit < 1 || limit > 200 {
		return nil, identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := readAsset(ctx, tx, owner, asset); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT rowid,document FROM asset_evidence WHERE owner=? AND asset_id=? AND (?=0 OR rowid<?) ORDER BY rowid DESC LIMIT ?", owner, asset, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []EvidenceAudit{}
	for rows.Next() {
		var item EvidenceAudit
		var raw []byte
		if err := rows.Scan(&item.Sequence, &raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item.Evidence); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
