package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func derivedMetadata(p mediastore.Publication) (mediastore.DerivedMetadata, string, error) {
	var m mediastore.DerivedMetadata
	if len(p.Target.Metadata) == 0 || len(p.Target.Metadata) > 4096 {
		return m, "", domain.Fail("INVALID_ARGUMENT", "Invalid derived publication")
	}
	d := json.NewDecoder(bytes.NewReader(p.Target.Metadata))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, "", err
	}
	if d.Decode(new(any)) != io.EOF {
		return m, "", domain.Fail("INVALID_ARGUMENT", "Invalid derived metadata")
	}
	key, err := m.Key()
	if err != nil {
		return m, "", err
	}
	valid := m.Transform == "thumbnail256/v1" && p.Kind == "thumbnail" && p.Object.MIME == "image/png" && p.Target.ID == m.ParentObjectID && p.Target.Generation == 1
	valid = valid || m.Transform == "scrape-document/v1" && p.Kind == "scrape-document" && p.Object.MIME == "application/json" && p.Target.ID == m.JobID && p.Target.Generation == m.Generation
	if !valid || p.Target.Purpose != "derived" || p.Kind != p.Object.Kind {
		return m, "", domain.Fail("INVALID_ARGUMENT", "Derived publication type mismatch")
	}
	return m, key, nil
}

func derivedSource(ctx context.Context, tx *sql.Tx, owner string, m mediastore.DerivedMetadata) error {
	if m.Transform == "thumbnail256/v1" {
		var state string
		var raw []byte
		err := tx.QueryRowContext(ctx, `SELECT c.state,c.document FROM media_content_objects c JOIN tracks t ON t.id=c.source_id AND t.owner=c.owner WHERE c.owner=? AND c.id=?`, owner, m.ParentObjectID).Scan(&state, &raw)
		if err != nil {
			return err
		}
		var object mediastore.ContentObject
		if err = json.Unmarshal(raw, &object); err != nil {
			return err
		}
		if state != "available" || object.Metadata.Field != "artwork" || object.SHA256 != m.ParentSHA256 {
			return domain.Fail("RESOURCE_CHANGED", "Thumbnail source is unavailable")
		}
		return nil
	}
	var targetRaw []byte
	var generation int64
	var targetState, jobState string
	err := tx.QueryRowContext(ctx, `SELECT t.target,t.generation,t.state,j.state FROM enrichment_targets t JOIN enrichment_jobs j ON j.id=t.job_id AND j.owner=t.owner JOIN tracks s ON s.id=t.source_id AND s.owner=t.owner WHERE t.owner=? AND t.job_id=? AND t.source_id=?`, owner, m.JobID, m.SourceTrackID).Scan(&targetRaw, &generation, &targetState, &jobState)
	if err != nil {
		return err
	}
	var target enrichment.Target
	if err = json.Unmarshal(targetRaw, &target); err != nil {
		return err
	}
	if target.AssetID != m.AssetID || target.AssetVersion != m.AssetVersion || generation != m.Generation || targetState != "running" || jobState != "running" {
		return domain.Fail("CONFLICT", "Scrape document attempt is no longer active")
	}
	return nil
}

func (s *Store) beginDerived(ctx context.Context, p mediastore.Publication) error {
	if !mediaID(p.ID) || !mediaID(p.Object.ID) || p.Owner == "" || p.Status != "prepared" || p.Object.Size != 0 || p.Object.SHA256 != "" || p.Object.RelativePath != "objects/"+p.Object.ID || p.CreatedAt.IsZero() {
		return domain.Fail("INVALID_ARGUMENT", "Invalid derived publication")
	}
	m, _, err := derivedMetadata(p)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = derivedSource(ctx, tx, p.Owner, m); err != nil {
		return err
	}
	if err = savePublication(ctx, tx, &p, true); err != nil {
		return err
	}
	return tx.Commit()
}

func referenceDerived(ctx context.Context, tx *sql.Tx, p mediastore.Publication) error {
	m, key, err := derivedMetadata(p)
	if err != nil {
		return err
	}
	if err = p.Object.Validate(); err != nil {
		return err
	}
	if err = derivedSource(ctx, tx, p.Owner, m); err != nil {
		return err
	}
	var exists int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM media_derived_objects WHERE owner=? AND relationship_key=? AND state='available'", p.Owner, key).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return domain.Fail("CONFLICT", "Derived object already published")
	}
	o := mediastore.DerivedObject{Object: p.Object, Metadata: m, State: "available", CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO media_derived_objects(id,owner,publication_id,relationship_key,transform,parent_object_id,job_id,source_id,generation,state,document,created_at) VALUES(?,?,?,?,?,?,?,?,?,'available',?,?)`, o.ID, p.Owner, p.ID, key, m.Transform, nullText(m.ParentObjectID), nullText(m.JobID), nullText(m.SourceTrackID), m.Generation, raw, o.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
