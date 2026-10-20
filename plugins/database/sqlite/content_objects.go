package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/mediastore/content"
)

func validateContentInput(input mediastore.UploadInput) error {
	if err := input.Metadata.Validate(); err != nil {
		return err
	}
	limit, mime := int64(content.MaxLyricsBytes), "text/plain; charset=utf-8"
	if input.Metadata.Field == "artwork" {
		limit, mime = content.MaxArtworkBytes, "image/"+input.Metadata.Format
	}
	hash, err := hex.DecodeString(input.SHA256)
	if input.Size < 1 || input.Size > limit || input.MIME != mime || err != nil || len(hash) != 32 || strings.ToLower(input.SHA256) != input.SHA256 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid validated media input")
	}
	return nil
}

func contentCurrentTarget(ctx context.Context, tx *sql.Tx, owner string, target mediastore.ContentTarget) error {
	actual, err := contentTarget(ctx, tx, owner, target.SourceTrackID)
	if err != nil {
		return err
	}
	if actual != target {
		return domain.Fail("RESOURCE_CHANGED", "Media target version changed")
	}
	var current bool
	if err = tx.QueryRowContext(ctx, "SELECT current FROM source_assets WHERE owner=? AND id=?", owner, target.AssetID).Scan(&current); err != nil {
		return err
	}
	if !current {
		return domain.Fail("RESOURCE_CHANGED", "Media target is no longer current")
	}
	return nil
}

func contentTarget(ctx context.Context, tx *sql.Tx, owner, source string) (mediastore.ContentTarget, error) {
	t := mediastore.ContentTarget{SourceTrackID: source}
	err := tx.QueryRowContext(ctx, "SELECT asset_id,version FROM tracks WHERE owner=? AND id=?", owner, source).Scan(&t.AssetID, &t.AssetVersion)
	if errors.Is(err, sql.ErrNoRows) {
		err = domain.Fail("NOT_FOUND", "Source track not found")
	}
	return t, err
}

func (s *Store) PrepareContent(ctx context.Context, owner string, input mediastore.UploadInput, key string) (mediastore.Publication, bool, error) {
	var empty mediastore.Publication
	input.Metadata.Language = contentLanguage(input.Metadata.Field, input.Metadata.Language)
	if owner == "" || len(key) < 8 || len(key) > 128 {
		return empty, false, domain.Fail("INVALID_ARGUMENT", "Invalid media operation")
	}
	if err := validateContentInput(input); err != nil {
		return empty, false, err
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return empty, false, err
	}
	sum := sha256.Sum256(raw)
	digest := hex.EncodeToString(sum[:])
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	if err = visibleSources(ctx, tx, owner, []string{input.Metadata.Target.SourceTrackID}); err != nil {
		return empty, false, err
	}
	prior, exists, err := contentIdempotency(ctx, tx, owner, "media/upload", key, digest)
	if err != nil {
		return empty, false, err
	}
	if exists {
		var id string
		if err = json.Unmarshal(prior, &id); err != nil {
			return empty, false, err
		}
		p, err := readPublication(tx.QueryRowContext(ctx, "SELECT document FROM media_publications WHERE owner=? AND id=?", owner, id))
		return p, true, err
	}
	if err = contentCurrentTarget(ctx, tx, owner, input.Metadata.Target); err != nil {
		return empty, false, err
	}
	objectID := domain.NewID()
	now := time.Now().UTC()
	p := mediastore.Publication{ID: domain.NewID(), Owner: owner, Kind: input.Metadata.Field, Status: "prepared", Object: mediastore.Object{ID: objectID, Kind: input.Metadata.Field, MIME: input.MIME, RelativePath: "objects/" + objectID}, Target: mediastore.Target{Purpose: "content", ID: objectID, Generation: 1, Metadata: raw}, CreatedAt: now, UpdatedAt: now}
	if err = savePublication(ctx, tx, &p, true); err != nil {
		return empty, false, err
	}
	result, err := json.Marshal(p.ID)
	if err != nil {
		return empty, false, err
	}
	if err = saveContentIdempotency(ctx, tx, owner, "media/upload", key, digest, result); err != nil {
		return empty, false, err
	}
	return p, false, tx.Commit()
}

func contentLanguage(field, language string) string {
	return mediastore.CanonicalLanguage(field, language)
}

func readContentObject(ctx context.Context, tx *sql.Tx, owner, id string) (mediastore.ContentObject, error) {
	var result mediastore.ContentObject
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT o.document,o.state FROM media_content_objects o JOIN tracks t ON t.id=o.source_id AND t.owner=o.owner WHERE o.owner=? AND o.id=?", owner, id).Scan(&raw, &result.State)
	if errors.Is(err, sql.ErrNoRows) {
		return result, domain.Fail("NOT_FOUND", "Media object not found")
	}
	if err != nil {
		return result, err
	}
	state := result.State
	if err = json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	result.State = state
	result.RelativePath = "objects/" + result.ID
	return result, result.Object.Validate()
}

func (s *Store) ContentObject(ctx context.Context, owner, id string) (mediastore.ContentObject, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return mediastore.ContentObject{}, err
	}
	defer tx.Rollback()
	return readContentObject(ctx, tx, owner, id)
}

func contentIdempotency(ctx context.Context, tx *sql.Tx, owner, route, key, digest string) ([]byte, bool, error) {
	var prior, expires string
	var result []byte
	err := tx.QueryRowContext(ctx, "SELECT digest,result,expires_at FROM idempotency WHERE owner=? AND route=? AND key=?", owner, route, key).Scan(&prior, &result, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if prior != digest {
		return nil, false, domain.Fail("CONFLICT", "Idempotency key was used for different media parameters")
	}
	until, err := time.Parse(time.RFC3339Nano, expires)
	if err != nil || time.Now().After(until) {
		return nil, false, domain.Fail("IDEMPOTENCY_EXPIRED", "Create a new media operation")
	}
	return result, true, nil
}

func saveContentIdempotency(ctx context.Context, tx *sql.Tx, owner, route, key, digest string, result []byte) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO idempotency(owner,route,key,digest,result,expires_at) VALUES(?,?,?,?,?,?)", owner, route, key, digest, result, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339Nano))
	return err
}
