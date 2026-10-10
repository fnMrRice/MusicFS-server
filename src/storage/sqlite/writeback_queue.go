package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

func writebackTrack(ctx context.Context, tx *sql.Tx, owner string, target writeback.Target) (library.Track, error) {
	t := library.Track{Owner: owner}
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document,external_id,root_id,path,version,mime,size FROM tracks WHERE owner=? AND id=?", owner, target.SourceTrackID).Scan(&raw, &t.ExternalID, &t.RootID, &t.Path, &t.Version, &t.MIME, &t.Size)
	if err == sql.ErrNoRows {
		return t, domain.Fail("NOT_FOUND", "Source track not found")
	}
	if err != nil {
		return t, err
	}
	if err = json.Unmarshal(raw, &t); err != nil {
		return t, err
	}
	if t.AssetID != target.AssetID || t.Version != target.AssetVersion || t.ProviderID != target.InstanceID || t.RootID != target.RootID || t.Path != target.RelativePath || t.Size <= 0 || t.Availability != "available" {
		return t, domain.Fail("RESOURCE_CHANGED", "Writeback source binding changed")
	}
	a, err := readAsset(ctx, tx, owner, target.AssetID)
	if err != nil {
		return t, err
	}
	if !a.Current || a.SourceTrackID != target.SourceTrackID || a.Version != target.AssetVersion || a.FileSizeBytes == nil || *a.FileSizeBytes != t.Size {
		return t, domain.Fail("RESOURCE_CHANGED", "Writeback asset changed")
	}
	t.Asset = &a
	return t, nil
}

func writebackObjects(ctx context.Context, tx *sql.Tx, owner string, r writebackhost.Request) ([]writebackhost.ObjectRef, mediastore.Selection, error) {
	objects := []writebackhost.ObjectRef{}
	selection, err := readContentSelection(ctx, tx, owner, r.SourceTrackID)
	if err != nil {
		return objects, selection, err
	}
	if selection.Revision != r.ExpectedRevision || selection.Target.AssetID != r.AssetID || selection.Target.AssetVersion != r.AssetVersion {
		return objects, selection, domain.Fail("CONFLICT", "Writeback selection revision changed")
	}
	for _, field := range r.Fields {
		b, _ := contentSelectedSlot(selection, field.Name, field.Language)
		if b == nil || !b.Current || b.State != "available" {
			return objects, selection, domain.Fail("CONFLICT", "Writeback requires current available selected content")
		}
		object, err := readContentObject(ctx, tx, owner, b.ObjectID)
		if err != nil {
			return objects, selection, err
		}
		if object.State != "available" || object.Metadata.Field != field.Name || object.Metadata.Language != field.Language {
			return objects, selection, domain.Fail("RESOURCE_CHANGED", "Writeback selected content changed")
		}
		objects = append(objects, writebackhost.ObjectRef{Field: field, ObjectID: object.ID})
	}
	return objects, selection, nil
}

func (s *Store) QueueWriteback(ctx context.Context, owner string, r writebackhost.Request, target writeback.Target, key string) (writebackhost.Job, bool, error) {
	var empty writebackhost.Job
	r, err := normalizeWriteback(r)
	if err != nil {
		return empty, false, err
	}
	if !validWritebackTarget(r, target) {
		return empty, false, domain.Fail("INVALID_ARGUMENT", "Invalid writeback target")
	}
	bound, err := writebackDigest(struct {
		Request writebackhost.Request
		Target  writeback.Target
	}{r, target})
	if err != nil {
		return empty, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return empty, false, err
	}
	defer tx.Rollback()
	prior, replay, binding, err := replayWriteback(ctx, tx, owner, r, key)
	if err != nil {
		return empty, false, err
	}
	if replay {
		if binding != bound {
			return empty, false, domain.Fail("CONFLICT", "Writeback operation target changed")
		}
		return prior, true, nil
	}
	t, err := writebackTrack(ctx, tx, owner, target)
	if err != nil {
		return empty, false, err
	}
	objects, _, err := writebackObjects(ctx, tx, owner, r)
	if err != nil {
		return empty, false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM writeback_jobs WHERE state IN ('queued','running','recovery')").Scan(&count); err != nil {
		return empty, false, err
	}
	if count >= 128 {
		return empty, false, domain.Fail("RATE_LIMITED", "Writeback queue is full")
	}
	var random [24]byte
	if _, err = rand.Read(random[:]); err != nil {
		return empty, false, err
	}
	now := time.Now().UTC()
	j := writebackhost.Job{ID: hex.EncodeToString(random[:]), Owner: owner, Request: r, Status: "queued", CreatedAt: now, UpdatedAt: now}
	w := writebackhost.Work{Job: j, Target: target, ExternalID: t.ExternalID, Objects: objects}
	raw, err := json.Marshal(w)
	if err != nil {
		return empty, false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO writeback_jobs(id,owner,source_id,state,old_size,work,created_at,updated_at) VALUES(?,?,?,'queued',?,?,?,?)", j.ID, owner, r.SourceTrackID, t.Size, raw, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return empty, false, err
	}
	digest, err := writebackDigest(r)
	if err != nil {
		return empty, false, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO writeback_operations(owner,key,request_digest,binding_digest,job_id,expires_at) VALUES(?,?,?,?,?,?)", owner, key, digest, bound, j.ID, now.Add(24*time.Hour).Format(time.RFC3339Nano))
	if err != nil {
		return empty, false, err
	}
	return j, false, tx.Commit()
}
