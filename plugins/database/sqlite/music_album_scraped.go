package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"slices"
	"strings"

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
	return s.saveMusicAlbumScraped(ctx, owner, id, revision, []works.ScrapedField{field}, key, "albums/scraped/"+field.Field, input, nil)
}

// SelectMusicAlbumScrapedFields 一次发布同一发行版的资料和完整曲目表。
func (s *Store) SelectMusicAlbumScrapedFields(ctx context.Context, owner, id string, revision int64, fields []works.ScrapedField, key string) (int64, error) {
	return s.selectMusicAlbumBundle(ctx, owner, id, revision, fields, key, nil)
}

// 自动采用发行版时，将证据检查与实际写入放入同一事务。
func (s *Store) SelectAutoMusicAlbumScrapedFields(ctx context.Context, owner, id, provider string, revision int64, fields []works.ScrapedField, key string) (int64, error) {
	if provider != "" && !works.ValidID(provider) {
		return 0, domain.Fail("INVALID_ARGUMENT", "Invalid provider identifier")
	}
	return s.selectMusicAlbumBundle(ctx, owner, id, revision, fields, key, &provider)
}

func (s *Store) selectMusicAlbumBundle(ctx context.Context, owner, id string, revision int64, fields []works.ScrapedField, key string, autoProvider *string) (int64, error) {
	if !works.ValidID(id) || len(fields) < 2 || len(fields) > 3 {
		return 0, domain.Fail("INVALID_ARGUMENT", "Album details and complete track list are required")
	}
	fields = slices.Clone(fields)
	slices.SortFunc(fields, func(a, b works.ScrapedField) int { return strings.Compare(a.Field, b.Field) })
	type fieldToken struct{ Field, PluginID, Token string }
	input := make([]fieldToken, len(fields))
	seen := map[string]bool{}
	for i, field := range fields {
		if !music.AlbumScrapedField(field.Field) || seen[field.Field] || !works.ValidID(field.PluginID) || !works.ValidID(field.CandidateToken) || field.PluginID != fields[0].PluginID {
			return 0, domain.Fail("INVALID_ARGUMENT", "Invalid album scraped field bundle")
		}
		seen[field.Field] = true
		input[i] = fieldToken{field.Field, field.PluginID, field.CandidateToken}
	}
	if !seen["albumDetails"] || !seen["albumTracks"] {
		return 0, domain.Fail("INVALID_ARGUMENT", "Album details and complete track list are required")
	}
	if autoProvider != nil {
		autoInput := struct {
			Provider string
			Fields   []fieldToken
		}{*autoProvider, input}
		return s.saveMusicAlbumScraped(ctx, owner, id, revision, fields, key, "albums/auto-scraped", autoInput, autoProvider)
	}
	return s.saveMusicAlbumScraped(ctx, owner, id, revision, fields, key, "albums/scraped", input, nil)
}

func (s *Store) saveMusicAlbumScraped(ctx context.Context, owner, id string, revision int64, fields []works.ScrapedField, key, kind string, input any, autoProvider *string) (int64, error) {
	return saveMusic(ctx, s, owner, kind, id, key, revision, input, func(tx *sql.Tx, albumID string) (int64, error) {
		album, err := readMusicAlbum(ctx, tx, owner, albumID)
		if err != nil {
			return 0, err
		}
		if album.Revision != revision {
			return 0, domain.Fail("CONFLICT", "Music album revision changed")
		}
		for _, field := range fields {
			if field.Item.ID == "" {
				return 0, domain.Fail("NOT_FOUND", "Scraper candidate expired or unavailable")
			}
			emptyManualList := autoProvider == nil && field.Field == "albumTracks" && len(field.Item.Tracks) == 0
			if len(fields) > 1 && (field.Item.ID != fields[0].Item.ID || emptyManualList) {
				return 0, domain.Fail("INVALID_ARGUMENT", "Album candidates must describe the same release and include its complete track list")
			}
			if field.Field == "albumTracks" && autoProvider != nil {
				evidence, err := readAlbumTrackEvidence(ctx, tx, owner, albumID, *autoProvider)
				if err != nil {
					return 0, err
				}
				if !music.MatchesAlbumTracks(evidence, field.Item.Tracks) {
					return 0, domain.Fail("ALBUM_TRACK_MISMATCH", "No source track in this album matches a release track title and artist")
				}
			}
		}
		for _, field := range fields {
			if len(fields) > 1 && field.Field == "albumDetails" {
				if err = setMusicAlbumSource(ctx, tx, album, field, true); err != nil {
					return 0, err
				}
			}
			if field.Field == "albumTracks" {
				if len(field.Item.Tracks) == 0 {
					return 0, domain.Fail("INVALID_ARGUMENT", "Album track list must not be empty")
				}
				if err = syncMusicAlbumScrapedTracks(ctx, tx, owner, albumID, field); err != nil {
					return 0, err
				}
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

// 初次明确选用发行版时保存对应页面；缺失或无效页面不破坏已有配对。
func setMusicAlbumSource(ctx context.Context, tx *sql.Tx, album music.Album, field works.ScrapedField, replace bool) error {
	link := music.AlbumSourceLink{PluginID: field.PluginID, URL: strings.TrimSpace(field.Item.SourceURL)}
	if link.URL == "" {
		return nil
	}
	if _, err := (music.AlbumFields{Name: "source", SourceLinks: []music.AlbumSourceLink{link}}).Normalize(); err != nil {
		return nil
	}
	i := slices.IndexFunc(album.SourceLinks, func(value music.AlbumSourceLink) bool { return value.PluginID == link.PluginID })
	if i < 0 {
		album.SourceLinks = append(album.SourceLinks, link)
	} else if replace {
		album.SourceLinks[i] = link
	} else {
		return nil
	}
	if _, err := album.AlbumFields.Normalize(); err != nil {
		return err
	}
	raw, err := json.Marshal(album.SourceLinks)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE music_albums SET source_links=? WHERE id=?", string(raw), album.ID)
	return err
}
