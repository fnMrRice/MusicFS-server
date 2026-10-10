package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func contentPublicationInput(p mediastore.Publication) (mediastore.UploadInput, error) {
	var input mediastore.UploadInput
	if p.Target.Purpose != "content" || p.Target.ID != p.Object.ID || p.Target.Generation != 1 || len(p.Target.Metadata) > 8192 {
		return input, domain.Fail("INVALID_ARGUMENT", "Invalid content publication")
	}
	decoder := json.NewDecoder(bytes.NewReader(p.Target.Metadata))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		return input, domain.Fail("INVALID_ARGUMENT", "Invalid content publication metadata")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return input, domain.Fail("INVALID_ARGUMENT", "Invalid content publication metadata")
	}
	if err := validateContentInput(input); err != nil {
		return input, err
	}
	if p.Kind != input.Metadata.Field || p.Object.Kind != p.Kind || p.Object.MIME != input.MIME {
		return input, domain.Fail("INVALID_ARGUMENT", "Content publication kind mismatch")
	}
	return input, nil
}

// Called inside the shared publication transaction after its phase CAS.
func referenceContent(ctx context.Context, tx *sql.Tx, p mediastore.Publication) error {
	input, err := contentPublicationInput(p)
	if err != nil {
		return err
	}
	if err = p.Object.Validate(); err != nil {
		return err
	}
	if p.Object.Size != input.Size || p.Object.SHA256 != input.SHA256 {
		return domain.Fail("RESOURCE_CHANGED", "Published content differs from the prepared bytes")
	}
	if err = contentCurrentTarget(ctx, tx, p.Owner, input.Metadata.Target); err != nil {
		return err
	}
	object := mediastore.ContentObject{Object: p.Object, Metadata: input.Metadata, State: "available", CreatedAt: time.Now().UTC()}
	raw, err := json.Marshal(object)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO media_content_objects(id,owner,source_id,asset_id,publication_id,state,document,created_at) VALUES(?,?,?,?,?,'available',?,?)", object.ID, p.Owner, object.Metadata.Target.SourceTrackID, object.Metadata.Target.AssetID, p.ID, raw, object.CreatedAt.Format(time.RFC3339Nano))
	return err
}

func (s *Store) InvalidateContent(ctx context.Context, owner, id string) error {
	var publication string
	err := s.DB.QueryRowContext(ctx, "SELECT o.publication_id FROM media_content_objects o JOIN tracks t ON t.id=o.source_id AND t.owner=o.owner WHERE o.owner=? AND o.id=?", owner, id).Scan(&publication)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Media object not found")
	}
	if err != nil {
		return err
	}
	return s.InvalidatePublication(ctx, owner, publication, "RESOURCE_CHANGED")
}

func invalidateContentPublication(ctx context.Context, tx *sql.Tx, p mediastore.Publication) error {
	if _, err := tx.ExecContext(ctx, "UPDATE media_content_objects SET state='unavailable' WHERE owner=? AND publication_id=?", p.Owner, p.ID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.source_id,s.field,s.language,s.binding_id,s.manual_locked FROM media_content_slots s JOIN media_content_bindings b ON b.id=s.binding_id AND b.owner=s.owner WHERE s.owner=? AND b.object_id=? ORDER BY s.source_id,s.field,s.language`, p.Owner, p.Object.ID)
	if err != nil {
		return err
	}
	type slot struct {
		source, field, language, binding string
		locked                           bool
	}
	var all []slot
	for rows.Next() {
		var v slot
		if err = rows.Scan(&v.source, &v.field, &v.language, &v.binding, &v.locked); err != nil {
			rows.Close()
			return err
		}
		all = append(all, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	revisions := map[string]int64{}
	for _, v := range all {
		revision, ok := revisions[v.source]
		if !ok {
			revision, err = advanceContentRevision(ctx, tx, p.Owner, v.source)
			if err != nil {
				return err
			}
			revisions[v.source] = revision
		}
		if err = recordContentChange(ctx, tx, p.Owner, v.source, revision, "unavailable", v.field, v.language, v.binding, v.binding, v.locked); err != nil {
			return err
		}
	}
	return nil
}
