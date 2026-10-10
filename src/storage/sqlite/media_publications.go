package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func mediaID(id string) bool {
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 24 && strings.ToLower(id) == id
}

func readPublication(row rowScanner) (mediastore.Publication, error) {
	var raw []byte
	var p mediastore.Publication
	if err := row.Scan(&raw); err != nil {
		return p, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, err
	}
	p.Object.RelativePath = "objects/" + p.Object.ID
	return p, nil
}

func savePublication(ctx context.Context, tx *sql.Tx, p *mediastore.Publication, insert bool) error {
	p.UpdatedAt = time.Now().UTC()
	p.CreatedAt = p.CreatedAt.UTC()
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if insert {
		_, err = tx.ExecContext(ctx, "INSERT INTO media_publications(id,owner,object_id,purpose,target_id,generation,status,document,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)", p.ID, p.Owner, p.Object.ID, p.Target.Purpose, p.Target.ID, p.Target.Generation, p.Status, raw, p.CreatedAt.Format(time.RFC3339Nano), p.UpdatedAt.Format(time.RFC3339Nano))
	} else {
		_, err = tx.ExecContext(ctx, "UPDATE media_publications SET status=?,document=?,updated_at=? WHERE id=? AND owner=?", p.Status, raw, p.UpdatedAt.Format(time.RFC3339Nano), p.ID, p.Owner)
	}
	return err
}

func publicationSummary(p mediastore.Publication, j identification.FingerprintJob) (identification.FingerprintSummary, error) {
	var summary identification.FingerprintSummary
	if len(p.Target.Metadata) == 0 || len(p.Target.Metadata) > 8192 {
		return summary, identification.Invalid()
	}
	decoder := json.NewDecoder(bytes.NewReader(p.Target.Metadata))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&summary); err != nil {
		return summary, identification.Invalid()
	}
	if decoder.Decode(new(any)) != io.EOF {
		return summary, identification.Invalid()
	}
	if err := summary.Validate(j.FingerprintTarget); err != nil {
		return summary, err
	}
	summary.ComputedAt = summary.ComputedAt.UTC()
	return summary, nil
}

func publicationJob(ctx context.Context, tx *sql.Tx, p mediastore.Publication) (identification.FingerprintJob, error) {
	j, err := readFingerprint(tx.QueryRowContext(ctx, "SELECT document FROM fingerprint_jobs WHERE id=? AND owner=?", p.Target.ID, p.Owner))
	if err != nil {
		return j, err
	}
	if p.Target.Purpose != "fingerprint" || j.Generation != p.Target.Generation || j.Status != "running" {
		return j, domain.Fail("CONFLICT", "Fingerprint publication generation is no longer active")
	}
	if err = currentFingerprintTarget(ctx, tx, j.Owner, j.FingerprintTarget); err != nil {
		return j, err
	}
	return j, nil
}

func (s *Store) BeginPublication(ctx context.Context, p mediastore.Publication) error {
	if !mediaID(p.ID) || !mediaID(p.Object.ID) || p.Owner == "" || p.Status != "prepared" || p.Kind != "fingerprint" || p.Object.Kind != p.Kind || p.Object.MIME != "application/json" || p.Object.Size != 0 || p.Object.SHA256 != "" || p.Object.RelativePath != "objects/"+p.Object.ID || p.CreatedAt.IsZero() {
		return identification.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	j, err := publicationJob(ctx, tx, p)
	if err != nil {
		return err
	}
	summary, err := publicationSummary(p, j)
	if err != nil {
		return err
	}
	p.Target.Metadata, err = json.Marshal(summary)
	if err != nil {
		return err
	}
	if err = savePublication(ctx, tx, &p, true); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetPublication(ctx context.Context, owner, id string) (mediastore.Publication, error) {
	p, err := readPublication(s.DB.QueryRowContext(ctx, "SELECT document FROM media_publications WHERE id=? AND owner=?", id, owner))
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Media publication not found")
	}
	return p, err
}

func (s *Store) WrittenPublication(ctx context.Context, owner, id string, o mediastore.Object) error {
	if err := o.Validate(); err != nil {
		return err
	}
	if o.Kind != "fingerprint" || o.MIME != "application/json" || o.Size > fingerprint.MaxDetailBytes {
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
	if p.Status == "written" && p.Object == o {
		return nil
	}
	if p.Status != "prepared" || p.Object.ID != o.ID || p.Object.Kind != o.Kind || p.Object.MIME != o.MIME {
		return domain.Fail("CONFLICT", "Media write does not match its prepared publication")
	}
	p.Object, p.Status = o, "written"
	if err = savePublication(ctx, tx, &p, false); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) SetPublicationStage(ctx context.Context, owner, id, from, to string) error {
	valid := from == "written" && to == "published" || from == "referenced" && to == "finished" || (from == "prepared" || from == "written" || from == "published") && to == "aborted"
	if !valid {
		return domain.Fail("INVALID_ARGUMENT", "Invalid media publication transition")
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
	if p.Status == to {
		return nil
	}
	if p.Status != from {
		return domain.Fail("CONFLICT", "Media publication phase changed")
	}
	p.Status = to
	if err = savePublication(ctx, tx, &p, false); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) PendingPublications(ctx context.Context, after string, limit int) ([]mediastore.Publication, error) {
	if limit < 1 || limit > 200 {
		return nil, identification.Invalid()
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT document FROM media_publications WHERE id>? AND status IN ('prepared','written','published','referenced') ORDER BY id LIMIT ?", after, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mediastore.Publication{}
	for rows.Next() {
		p, err := readPublication(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}
