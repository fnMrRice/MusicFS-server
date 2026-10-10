package sqlite

import (
	"context"
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (s *Store) CreateSourceAlbum(ctx context.Context, owner string, request albums.CreateSourceAlbumRequest, key string) (albums.SourceAlbum, error) {
	var result albums.SourceAlbum
	var err error
	if !albums.ValidID(request.ProviderInstanceID) {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid album provider")
	}
	if request.Name, err = albums.NormalizeName(request.Name); err != nil {
		return result, err
	}
	if request.SourceTrackIDs, err = albums.NormalizeMembers(request.SourceTrackIDs, albums.MaxAlbumSources); err != nil {
		return result, err
	}
	m, err := s.beginAlbumMutation(ctx, owner, "source/create", key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		result.Owner = owner
		return result, err
	}
	if err = validateAlbumSources(ctx, m.tx, owner, request.ProviderInstanceID, request.SourceTrackIDs); err != nil {
		return result, err
	}
	now := time.Now().UTC()
	result = albums.SourceAlbum{ID: domain.NewID(), Owner: owner, ProviderInstanceID: request.ProviderInstanceID, Name: request.Name, Revision: 1, SourceTrackIDs: request.SourceTrackIDs, CreatedAt: now, UpdatedAt: now}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO source_albums VALUES(?,?,?,?,?,?,?)", result.ID, owner, result.ProviderInstanceID, result.Name, result.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	if err = replaceAlbumSources(ctx, m.tx, result.ID, result.SourceTrackIDs); err != nil {
		return result, err
	}
	if err = recordAlbumChange(ctx, m.tx, owner, albums.Change{Target: albums.Target{Kind: "sourceAlbum", ID: result.ID}, Area: "members", Revision: 1, Operation: "create", Members: result.SourceTrackIDs}); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}
func (s *Store) UpdateSourceAlbum(ctx context.Context, owner, id string, request albums.UpdateSourceAlbumRequest, key string) (albums.SourceAlbum, error) {
	var result albums.SourceAlbum
	var err error
	if !albums.ValidID(id) || request.ExpectedRevision < 1 {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid album revision")
	}
	if request.Name, err = albums.NormalizeName(request.Name); err != nil {
		return result, err
	}
	if request.SourceTrackIDs, err = albums.NormalizeMembers(request.SourceTrackIDs, albums.MaxAlbumSources); err != nil {
		return result, err
	}
	m, err := s.beginAlbumMutation(ctx, owner, "source/"+id+"/update", key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		result.Owner = owner
		return result, err
	}
	if result, err = readSourceAlbum(ctx, m.tx, owner, id); err != nil {
		return result, err
	}
	if result.Revision != request.ExpectedRevision {
		return result, domain.Fail("CONFLICT", "Source album revision changed")
	}
	dependents, err := albumDependentReleases(ctx, m.tx, owner, id)
	if err != nil {
		return result, err
	}
	if err = validateAlbumSources(ctx, m.tx, owner, result.ProviderInstanceID, request.SourceTrackIDs); err != nil {
		return result, err
	}
	previous := result.SourceTrackIDs
	result.Name, result.SourceTrackIDs, result.UpdatedAt = request.Name, request.SourceTrackIDs, time.Now().UTC()
	result.Revision++
	_, err = m.tx.ExecContext(ctx, "UPDATE source_albums SET name=?,revision=?,updated_at=? WHERE owner=? AND id=?", result.Name, result.Revision, result.UpdatedAt.Format(time.RFC3339Nano), owner, id)
	if err != nil {
		return result, err
	}
	if err = replaceAlbumSources(ctx, m.tx, id, result.SourceTrackIDs); err != nil {
		return result, err
	}
	if err = advanceAlbumDependents(ctx, m.tx, owner, dependents); err != nil {
		return result, err
	}
	if err = recordAlbumChange(ctx, m.tx, owner, albums.Change{Target: albums.Target{Kind: "sourceAlbum", ID: id}, Area: "members", Revision: result.Revision, Operation: "update", PreviousMembers: previous, Members: result.SourceTrackIDs}); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}
func (s *Store) CreateRelease(ctx context.Context, owner string, request albums.CreateReleaseRequest, key string) (albums.Release, error) {
	var result albums.Release
	var err error
	if request.Name, err = albums.NormalizeName(request.Name); err != nil {
		return result, err
	}
	if request.SourceAlbumIDs, err = albums.NormalizeMembers(request.SourceAlbumIDs, albums.MaxReleaseAlbums); err != nil {
		return result, err
	}
	m, err := s.beginAlbumMutation(ctx, owner, "release/create", key, request)
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
	result = albums.Release{ID: domain.NewID(), Owner: owner, Name: request.Name, Revision: 1, SourceAlbumIDs: request.SourceAlbumIDs, CreatedAt: now, UpdatedAt: now}
	_, err = m.tx.ExecContext(ctx, "INSERT INTO releases VALUES(?,?,?,?,?,?)", result.ID, owner, result.Name, result.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return result, err
	}
	if err = replaceReleaseAlbums(ctx, m.tx, owner, result.ID, result.SourceAlbumIDs); err != nil {
		return result, err
	}
	if err = recordAlbumChange(ctx, m.tx, owner, albums.Change{Target: albums.Target{Kind: "release", ID: result.ID}, Area: "members", Revision: 1, Operation: "create", Members: result.SourceAlbumIDs}); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}
func (s *Store) UpdateRelease(ctx context.Context, owner, id string, request albums.UpdateReleaseRequest, key string) (albums.Release, error) {
	var result albums.Release
	var err error
	if !albums.ValidID(id) || request.ExpectedRevision < 1 {
		return result, domain.Fail("INVALID_ARGUMENT", "Invalid release revision")
	}
	if request.Name, err = albums.NormalizeName(request.Name); err != nil {
		return result, err
	}
	if request.SourceAlbumIDs, err = albums.NormalizeMembers(request.SourceAlbumIDs, albums.MaxReleaseAlbums); err != nil {
		return result, err
	}
	m, err := s.beginAlbumMutation(ctx, owner, "release/"+id+"/update", key, request)
	if err != nil {
		return result, err
	}
	defer m.tx.Rollback()
	if len(m.prior) > 0 {
		err = json.Unmarshal(m.prior, &result)
		result.Owner = owner
		return result, err
	}
	if result, err = readRelease(ctx, m.tx, owner, id); err != nil {
		return result, err
	}
	if result.Revision != request.ExpectedRevision {
		return result, domain.Fail("CONFLICT", "Release revision changed")
	}
	previous := result.SourceAlbumIDs
	result.Name, result.SourceAlbumIDs, result.UpdatedAt = request.Name, request.SourceAlbumIDs, time.Now().UTC()
	result.Revision++
	_, err = m.tx.ExecContext(ctx, "UPDATE releases SET name=?,revision=?,updated_at=? WHERE owner=? AND id=?", result.Name, result.Revision, result.UpdatedAt.Format(time.RFC3339Nano), owner, id)
	if err != nil {
		return result, err
	}
	if err = replaceReleaseAlbums(ctx, m.tx, owner, id, result.SourceAlbumIDs); err != nil {
		return result, err
	}
	if err = recordAlbumChange(ctx, m.tx, owner, albums.Change{Target: albums.Target{Kind: "release", ID: id}, Area: "members", Revision: result.Revision, Operation: "update", PreviousMembers: previous, Members: result.SourceAlbumIDs}); err != nil {
		return result, err
	}
	return result, m.finish(ctx, result)
}
