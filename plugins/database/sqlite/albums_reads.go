package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func albumNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Fail("NOT_FOUND", "Album or release not found")
	}
	return err
}
func readAlbumIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
		if len(ids) > albums.MaxAlbumSources {
			return nil, domain.Fail("RATE_LIMITED", "Album membership exceeds its limit")
		}
	}
	return ids, rows.Err()
}

// Queueing an album scrape calls this helper in the queue transaction, then
// binds the returned revision and each current asset before committing work.
func readSourceAlbum(ctx context.Context, tx *sql.Tx, owner, id string) (albums.SourceAlbum, error) {
	a := albums.SourceAlbum{ID: id, Owner: owner, SourceTrackIDs: []string{}}
	var created, updated string
	err := tx.QueryRowContext(ctx, "SELECT provider_id,name,revision,created_at,updated_at FROM source_albums WHERE owner=? AND id=?", owner, id).Scan(&a.ProviderInstanceID, &a.Name, &a.Revision, &created, &updated)
	if err != nil {
		return a, albumNotFound(err)
	}
	if a.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return a, err
	}
	if a.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return a, err
	}
	a.SourceTrackIDs, err = readAlbumIDs(ctx, tx, "SELECT m.source_id FROM source_album_members m JOIN tracks t ON t.id=m.source_id WHERE m.album_id=? AND t.owner=? AND t.provider=? ORDER BY m.position", id, owner, a.ProviderInstanceID)
	return a, err
}
func readRelease(ctx context.Context, tx *sql.Tx, owner, id string) (albums.Release, error) {
	r := albums.Release{ID: id, Owner: owner, SourceAlbumIDs: []string{}}
	var created, updated string
	err := tx.QueryRowContext(ctx, "SELECT name,revision,created_at,updated_at FROM releases WHERE owner=? AND id=?", owner, id).Scan(&r.Name, &r.Revision, &created, &updated)
	if err != nil {
		return r, albumNotFound(err)
	}
	if r.CreatedAt, err = time.Parse(time.RFC3339Nano, created); err != nil {
		return r, err
	}
	if r.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated); err != nil {
		return r, err
	}
	r.SourceAlbumIDs, err = readAlbumIDs(ctx, tx, "SELECT m.album_id FROM release_albums m JOIN source_albums a ON a.id=m.album_id WHERE m.release_id=? AND a.owner=? ORDER BY m.position", id, owner)
	return r, err
}
func (s *Store) SourceAlbum(ctx context.Context, owner, id string) (albums.SourceAlbum, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return albums.SourceAlbum{}, err
	}
	defer tx.Rollback()
	return readSourceAlbum(ctx, tx, owner, id)
}
func (s *Store) Release(ctx context.Context, owner, id string) (albums.Release, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return albums.Release{}, err
	}
	defer tx.Rollback()
	return readRelease(ctx, tx, owner, id)
}
func albumPage(after string, limit int) error {
	if (after != "" && !albums.ValidID(after)) || limit < 1 || limit > albums.MaxPageSize+1 {
		return domain.Fail("INVALID_ARGUMENT", "Invalid album page")
	}
	return nil
}
func (s *Store) SourceAlbums(ctx context.Context, owner, provider, after string, limit int) ([]albums.SourceAlbum, error) {
	if err := albumPage(after, limit); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids, err := readAlbumIDs(ctx, tx, ordinaryMusicSources+"SELECT a.id FROM source_albums a WHERE a.owner=? AND (?='' OR a.provider_id=?) AND a.id>? AND "+ordinaryMusicAlbum+" ORDER BY a.id LIMIT ?", owner, owner, provider, provider, after, limit)
	if err != nil {
		return nil, err
	}
	result := []albums.SourceAlbum{}
	indices := map[string]int{}
	for _, id := range ids {
		a, err := readSourceAlbum(ctx, tx, owner, id)
		if err != nil {
			return nil, err
		}
		a.SourceTrackIDs = []string{}
		indices[id] = len(result)
		result = append(result, a)
	}
	if len(ids) == 0 {
		return result, nil
	}
	// 整页共用一次排除集合，避免为每张专辑重复解析全部作品包。
	rawIDs, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, ordinaryMusicSources+`SELECT m.album_id,t.id FROM source_album_members m
 JOIN source_albums a ON a.id=m.album_id JOIN tracks t ON t.id=m.source_id AND t.owner=a.owner AND t.provider=a.provider_id
 WHERE a.owner=? AND a.id IN (SELECT value FROM json_each(?)) AND `+ordinaryMusicTrack+` ORDER BY m.album_id,m.position`, owner, owner, string(rawIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var album, source string
		if err = rows.Scan(&album, &source); err != nil {
			return nil, err
		}
		a := &result[indices[album]]
		a.SourceTrackIDs = append(a.SourceTrackIDs, source)
	}
	return result, rows.Err()
}
func (s *Store) Releases(ctx context.Context, owner, after string, limit int) ([]albums.Release, error) {
	if err := albumPage(after, limit); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ids, err := readAlbumIDs(ctx, tx, "SELECT id FROM releases WHERE owner=? AND id>? ORDER BY id LIMIT ?", owner, after, limit)
	if err != nil {
		return nil, err
	}
	result := []albums.Release{}
	for _, id := range ids {
		r, err := readRelease(ctx, tx, owner, id)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, nil
}
func albumTargetRevision(ctx context.Context, tx *sql.Tx, owner string, target albums.Target) (int64, error) {
	if err := target.Validate(); err != nil {
		return 0, err
	}
	table := "source_albums"
	if target.Kind == "release" {
		table = "releases"
	}
	var revision int64
	err := tx.QueryRowContext(ctx, "SELECT revision FROM "+table+" WHERE owner=? AND id=?", owner, target.ID).Scan(&revision)
	return revision, albumNotFound(err)
}
func (s *Store) ExpandSourceAlbum(ctx context.Context, owner, id string, expected int64) (albums.Expansion, error) {
	result := albums.Expansion{Targets: []mediastore.ContentTarget{}}
	if expected < 1 {
		return result, domain.Fail("INVALID_ARGUMENT", "Expected album revision is required")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if result.Album, err = readSourceAlbum(ctx, tx, owner, id); err != nil {
		return result, err
	}
	if result.Album.Revision != expected {
		return result, domain.Fail("CONFLICT", "Source album revision changed")
	}
	for _, source := range result.Album.SourceTrackIDs {
		target, err := contentTarget(ctx, tx, owner, source)
		if err != nil {
			return result, err
		}
		if err = contentCurrentTarget(ctx, tx, owner, target); err != nil {
			return result, err
		}
		result.Targets = append(result.Targets, target)
	}
	return result, nil
}
