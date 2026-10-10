package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
)

func readAlbumCover(ctx context.Context, tx *sql.Tx, owner string, target albums.Target) (albums.CoverSelection, error) {
	result := albums.CoverSelection{Target: target}
	if _, err := albumTargetRevision(ctx, tx, owner, target); err != nil {
		return result, err
	}
	var binding albums.CoverBinding
	var created string
	err := tx.QueryRowContext(ctx, `SELECT h.revision,h.manual_locked,b.id,b.object_id,b.target_revision,b.source_id,b.asset_id,b.asset_version,b.created_at
 FROM album_cover_heads h JOIN album_cover_bindings b ON b.id=h.binding_id AND b.owner=h.owner AND b.target_kind=h.target_kind AND b.target_id=h.target_id
 WHERE h.owner=? AND h.target_kind=? AND h.target_id=?`, owner, target.Kind, target.ID).Scan(&result.Revision, &result.ManualLocked, &binding.ID, &binding.ObjectID, &binding.TargetRevision, &binding.SourceTrackID, &binding.AssetID, &binding.AssetVersion, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	if binding.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return result, err
	}
	// Membership and asset changes do not erase the selected edition cover.
	// Its immutable original object still requires the same owner/source access.
	object, err := readContentObject(ctx, tx, owner, binding.ObjectID)
	if err != nil {
		return result, err
	}
	if object.Metadata.Field != "artwork" || object.Metadata.Target != (mediastore.ContentTarget{SourceTrackID: binding.SourceTrackID, AssetID: binding.AssetID, AssetVersion: binding.AssetVersion}) {
		return result, domain.Fail("CONFLICT", "Album cover provenance is inconsistent")
	}
	binding.State = object.State
	result.Binding = &binding
	return result, nil
}
func (s *Store) Cover(ctx context.Context, owner string, target albums.Target) (albums.CoverSelection, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return albums.CoverSelection{}, err
	}
	defer tx.Rollback()
	return readAlbumCover(ctx, tx, owner, target)
}
func albumCoverMember(ctx context.Context, tx *sql.Tx, owner string, target albums.Target, source string) error {
	query := `SELECT COUNT(*) FROM source_album_members m JOIN source_albums a ON a.id=m.album_id JOIN tracks t ON t.id=m.source_id
 WHERE a.owner=? AND a.id=? AND m.source_id=? AND t.owner=a.owner AND t.provider=a.provider_id`
	if target.Kind == "release" {
		query = `SELECT COUNT(*) FROM release_albums r JOIN source_albums a ON a.id=r.album_id JOIN source_album_members m ON m.album_id=a.id JOIN tracks t ON t.id=m.source_id
 WHERE a.owner=? AND r.release_id=? AND m.source_id=? AND t.owner=a.owner AND t.provider=a.provider_id`
	}
	var count int
	if err := tx.QueryRowContext(ctx, query, owner, target.ID, source).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return domain.Fail("INVALID_ARGUMENT", "Artwork source is outside the album or release")
	}
	return nil
}
func (s *Store) SelectCover(ctx context.Context, owner string, r albums.SelectCoverRequest, key string, guard mediastore.Guard) (albums.CoverSelection, error) {
	var empty albums.CoverSelection
	if err := r.Target.Validate(); err != nil {
		return empty, err
	}
	if guard == nil || !albums.ValidID(r.ObjectID) || r.ExpectedRevision < 0 || r.ExpectedTargetRevision < 1 {
		return empty, domain.Fail("INVALID_ARGUMENT", "Invalid album cover selection")
	}
	m, err := s.beginAlbumMutation(ctx, owner, "cover/select", key, struct {
		Request   albums.SelectCoverRequest
		Automatic bool
	}{r, r.Automatic})
	if err != nil {
		return empty, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &empty)
		return empty, err
	}
	revision, err := albumTargetRevision(ctx, m.tx, owner, r.Target)
	if err != nil {
		return empty, err
	}
	if revision != r.ExpectedTargetRevision {
		return empty, domain.Fail("CONFLICT", "Album membership revision changed")
	}
	before, err := readAlbumCover(ctx, m.tx, owner, r.Target)
	if err != nil {
		return empty, err
	}
	if before.Revision != r.ExpectedRevision {
		return empty, domain.Fail("CONFLICT", "Album cover revision changed")
	}
	if r.Automatic && (before.ManualLocked || before.Binding != nil) {
		return empty, domain.Fail("CONFLICT", "Automatic artwork cannot replace an existing or locked album cover")
	}
	object, err := readContentObject(ctx, m.tx, owner, r.ObjectID)
	if err != nil {
		return empty, err
	}
	if object.State != "available" {
		return empty, domain.Fail("RESOURCE_CHANGED", "Artwork is unavailable")
	}
	if object.Metadata.Field != "artwork" {
		return empty, domain.Fail("INVALID_ARGUMENT", "Album covers require artwork")
	}
	if err = albumCoverMember(ctx, m.tx, owner, r.Target, object.Metadata.Target.SourceTrackID); err != nil {
		return empty, err
	}
	if err = contentCurrentTarget(ctx, m.tx, owner, object.Metadata.Target); err != nil {
		return empty, err
	}
	var result albums.CoverSelection
	var publishErr error
	called := false
	err = guard(func() error {
		if called {
			return domain.Fail("CONFLICT", "Album cover was already published")
		}
		called = true
		result, publishErr = m.selectAlbumCover(ctx, r, before, object)
		return publishErr
	})
	if err != nil {
		return empty, err
	}
	if !called {
		return empty, domain.Fail("CONFLICT", "Album cover guard did not publish")
	}
	if publishErr != nil {
		return empty, publishErr
	}
	return result, nil
}
func (m *albumMutation) selectAlbumCover(ctx context.Context, r albums.SelectCoverRequest, before albums.CoverSelection, object mediastore.ContentObject) (albums.CoverSelection, error) {
	var result albums.CoverSelection
	now := time.Now().UTC()
	b := albums.CoverBinding{ID: domain.NewID(), ObjectID: object.ID, TargetRevision: r.ExpectedTargetRevision, SourceTrackID: object.Metadata.Target.SourceTrackID, AssetID: object.Metadata.Target.AssetID, AssetVersion: object.Metadata.Target.AssetVersion, State: object.State, CreatedAt: now}
	_, err := m.tx.ExecContext(ctx, "INSERT INTO album_cover_bindings VALUES(?,?,?,?,?,?,?,?,?,?)", b.ID, m.owner, r.Target.Kind, r.Target.ID, b.ObjectID, b.TargetRevision, b.SourceTrackID, b.AssetID, b.AssetVersion, now.Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	_, err = m.tx.ExecContext(ctx, `INSERT INTO album_cover_heads VALUES(?,?,?,?,?,?) ON CONFLICT(owner,target_kind,target_id) DO UPDATE SET revision=excluded.revision,binding_id=excluded.binding_id,manual_locked=excluded.manual_locked`, m.owner, r.Target.Kind, r.Target.ID, before.Revision+1, b.ID, !r.Automatic)
	if err != nil {
		return result, err
	}
	previous := ""
	if before.Binding != nil {
		previous = before.Binding.ID
	}
	locked := !r.Automatic
	if err = recordAlbumChange(ctx, m.tx, m.owner, albums.Change{Target: r.Target, Area: "cover", Revision: before.Revision + 1, Operation: "selectCover", PreviousBindingID: strptr(previous), BindingID: &b.ID, ManualLocked: &locked}); err != nil {
		return result, err
	}
	result = albums.CoverSelection{Target: r.Target, Revision: before.Revision + 1, ManualLocked: locked, Binding: &b}
	return result, m.finish(ctx, result)
}
func (s *Store) UnlockCover(ctx context.Context, owner string, r albums.UnlockCoverRequest, key string) (albums.CoverSelection, error) {
	var result albums.CoverSelection
	if err := r.Target.Validate(); err != nil {
		return result, err
	}
	if r.ExpectedRevision < 0 {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid cover revision")
	}
	m, err := s.beginAlbumMutation(ctx, owner, "cover/unlock", key, r)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		return result, err
	}
	if result, err = readAlbumCover(ctx, m.tx, owner, r.Target); err != nil {
		return result, err
	}
	if result.Revision != r.ExpectedRevision {
		return result, domain.Fail("CONFLICT", "Album cover revision changed")
	}
	if result.Binding == nil {
		return result, domain.Fail("NOT_FOUND", "Album cover is not selected")
	}
	result.Revision++
	result.ManualLocked = false
	_, err = m.tx.ExecContext(ctx, "UPDATE album_cover_heads SET revision=?,manual_locked=0 WHERE owner=? AND target_kind=? AND target_id=?", result.Revision, owner, r.Target.Kind, r.Target.ID)
	if err != nil {
		return result, err
	}
	locked := false
	if err = recordAlbumChange(ctx, m.tx, owner, albums.Change{Target: r.Target, Area: "cover", Revision: result.Revision, Operation: "unlockCover", PreviousBindingID: &result.Binding.ID, BindingID: &result.Binding.ID, ManualLocked: &locked}); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}

var _ albums.Repository = (*Store)(nil)
