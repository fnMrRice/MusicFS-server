package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

type workMutation struct {
	tx                        *sql.Tx
	owner, route, key, digest string
	prior                     []byte
}

func (s *Store) beginWorkMutation(ctx context.Context, owner, route, key string, request any) (*workMutation, error) {
	if !works.ValidID(owner) || !works.ValidID(key) || len(key) < 8 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid work operation")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	m := &workMutation{tx: tx, owner: owner, route: "works/" + route, key: key, digest: hex.EncodeToString(sum[:])}
	m.prior, _, err = contentIdempotency(ctx, tx, owner, m.route, key, m.digest)
	if err != nil {
		tx.Rollback()
		return nil, err
	}
	return m, nil
}

func (m *workMutation) finish(ctx context.Context, result works.Work) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if err = saveContentIdempotency(ctx, m.tx, m.owner, m.route, m.key, m.digest, raw); err != nil {
		return err
	}
	return m.tx.Commit()
}

func (s *Store) CreateWork(ctx context.Context, owner string, request works.CreateRequest, key string) (works.Work, error) {
	var result works.Work
	request, err := works.NormalizeCreate(request)
	if err != nil {
		return result, err
	}
	m, err := s.beginWorkMutation(ctx, owner, "create", key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		result.Owner = owner
		return result, err
	}
	now := time.Now().UTC()
	result = works.Work{ID: domain.NewID(), Owner: owner, ContentType: request.ContentType, ShowInTrackList: request.ShowInTrackList, Title: request.Title, Description: request.Description, ExternalIDs: request.ExternalIDs, Credits: request.Credits, Tags: request.Tags, Revision: 1, CreatedAt: now, UpdatedAt: now}
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	if _, err = m.tx.ExecContext(ctx, "INSERT INTO works(id,owner,content_type,title,description,revision,show_in_track_list,document) VALUES(?,?,?,?,?,?,?,?)", result.ID, owner, result.ContentType, result.Title, result.Description, result.Revision, result.ShowInTrackList, raw); err != nil {
		return result, err
	}
	if err = replaceWorkMetadata(ctx, m.tx, result); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}

func (s *Store) UpdateWork(ctx context.Context, owner, id string, request works.UpdateRequest, key string) (works.Work, error) {
	var result works.Work
	if !works.ValidID(id) {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid work identifier")
	}
	request, err := works.NormalizeUpdate(request)
	if err != nil {
		return result, err
	}
	m, err := s.beginWorkMutation(ctx, owner, id+"/update", key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		result.Owner = owner
		return result, err
	}
	if result, err = readWork(ctx, m.tx, owner, id); err != nil {
		return result, err
	}
	if result.Revision != request.ExpectedRevision {
		return result, domain.Fail("CONFLICT", "Work revision changed")
	}
	visibilityChanged := result.ShowInTrackList != request.ShowInTrackList
	result.ShowInTrackList = request.ShowInTrackList
	result.Title, result.Description = request.Title, request.Description
	result.ExternalIDs, result.Credits, result.Tags = request.ExternalIDs, request.Credits, request.Tags
	result.Revision++
	result.UpdatedAt = time.Now().UTC()
	raw, err := json.Marshal(result)
	if err != nil {
		return result, err
	}
	changed, err := m.tx.ExecContext(ctx, "UPDATE works SET title=?,description=?,revision=?,show_in_track_list=?,document=? WHERE owner=? AND id=? AND revision=?", result.Title, result.Description, result.Revision, result.ShowInTrackList, raw, owner, id, request.ExpectedRevision)
	if err != nil {
		return result, err
	}
	count, err := changed.RowsAffected()
	if err != nil {
		return result, err
	}
	if count != 1 {
		return result, domain.Fail("CONFLICT", "Work revision changed")
	}
	if visibilityChanged {
		if _, err = m.tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1"); err != nil {
			return result, err
		}
	}
	if err = replaceWorkMetadata(ctx, m.tx, result); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}

func replaceWorkMetadata(ctx context.Context, tx *sql.Tx, work works.Work) error {
	for _, table := range []string{"work_external_ids", "work_credits", "work_tags"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table+" WHERE work_id=?", work.ID); err != nil {
			return err
		}
	}
	for _, item := range work.ExternalIDs {
		if _, err := tx.ExecContext(ctx, "INSERT INTO work_external_ids(work_id,namespace,value) VALUES(?,?,?)", work.ID, item.Namespace, item.Value); err != nil {
			return err
		}
	}
	for _, item := range work.Credits {
		if _, err := tx.ExecContext(ctx, "INSERT INTO work_credits(work_id,role,name) VALUES(?,?,?)", work.ID, item.Role, item.Name); err != nil {
			return err
		}
	}
	for _, tag := range work.Tags {
		if _, err := tx.ExecContext(ctx, "INSERT INTO work_tags(work_id,tag) VALUES(?,?)", work.ID, tag); err != nil {
			return err
		}
	}
	return nil
}
