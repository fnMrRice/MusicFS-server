package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func (s *Store) MusicAlbumScrapedFields(ctx context.Context, owner, id string) ([]works.ScrapedField, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err = readMusicAlbum(ctx, tx, owner, id); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, "SELECT document FROM music_album_scraped_fields WHERE album_id=? ORDER BY field", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fields := []works.ScrapedField{}
	for rows.Next() {
		var raw []byte
		var field works.ScrapedField
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &field); err != nil {
			return nil, err
		}
		fields = append(fields, field)
	}
	return fields, rows.Err()
}

func (s *Store) SelectMusicAlbumScrapedField(ctx context.Context, owner, id string, revision int64, field works.ScrapedField, key string) (int64, error) {
	if !works.ValidID(id) || !music.AlbumScrapedField(field.Field) || !works.ValidID(field.PluginID) || !works.ValidID(field.CandidateToken) {
		return 0, domain.Fail("INVALID_ARGUMENT", "Invalid album scraped field selection")
	}
	// 幂等身份仅包含原请求；候选过期后仍能重放成功回执。
	input := struct{ PluginID, Token string }{field.PluginID, field.CandidateToken}
	return saveMusic(ctx, s, owner, "albums/scraped/"+field.Field, id, key, revision, input, func(tx *sql.Tx, albumID string) (int64, error) {
		album, err := readMusicAlbum(ctx, tx, owner, albumID)
		if err != nil {
			return 0, err
		}
		if album.Revision != revision {
			return 0, domain.Fail("CONFLICT", "Music album revision changed")
		}
		if field.Item.ID == "" {
			return 0, domain.Fail("NOT_FOUND", "Scraper candidate expired or unavailable")
		}
		field.CandidateToken = ""
		raw, err := json.Marshal(field)
		if err != nil {
			return 0, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO music_album_scraped_fields(album_id,field,document) VALUES(?,?,?)
 ON CONFLICT(album_id,field) DO UPDATE SET document=excluded.document`, albumID, field.Field, string(raw)); err != nil {
			return 0, err
		}
		result, err := tx.ExecContext(ctx, "UPDATE music_albums SET revision=revision+1 WHERE owner=? AND id=? AND revision=?", owner, albumID, revision)
		if err != nil {
			return 0, err
		}
		if err = checkMusicUpdate(ctx, tx, result, owner, albumID, "music_albums"); err != nil {
			return 0, err
		}
		return revision + 1, nil
	})
}
