package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

const artistNameColumns = `a.id,a.name,a.names_revision,(SELECT json_group_array(json_object('language',n.language,'title',n.name) ORDER BY n.language,n.name)
 FROM music_artist_localized_names n WHERE n.artist_id=a.id)`

// 上游署名先采用已有的精确主名，再尝试唯一的别名。多个歌手共用一个别名时保留原署名。
func scrapedCreditNames(ctx context.Context, tx *sql.Tx, owner string, names []string) ([]string, error) {
	credits := make([]string, 0, len(names))
	for _, name := range names {
		var canonical string
		err := tx.QueryRowContext(ctx, `SELECT coalesce((SELECT name FROM music_artists WHERE owner=? AND name=?),
 (SELECT min(a.name) FROM music_artists a WHERE a.owner=? AND (a.name COLLATE NOCASE=? COLLATE NOCASE OR EXISTS(
  SELECT 1 FROM music_artist_localized_names n WHERE n.artist_id=a.id AND n.name COLLATE NOCASE=? COLLATE NOCASE)) HAVING count(*)=1),?)`,
			owner, name, owner, name, name, name).Scan(&canonical)
		if err != nil {
			return nil, err
		}
		if !slices.Contains(credits, canonical) {
			credits = append(credits, canonical)
		}
	}
	return credits, nil
}

func scanArtistNames(row musicScanner, artist *music.Artist, trackID *string, extra ...any) error {
	var names string
	fields := []any{&artist.ID, &artist.Name, &artist.Revision, &names}
	if trackID != nil {
		fields = append([]any{trackID}, fields...)
	}
	if err := row.Scan(append(fields, extra...)...); err != nil {
		return err
	}
	return json.Unmarshal([]byte(names), &artist.LocalizedNames)
}

func readArtistNames(ctx context.Context, db interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, owner, id string) (music.Artist, error) {
	var artist music.Artist
	err := scanArtistNames(db.QueryRowContext(ctx, "SELECT "+artistNameColumns+" FROM music_artists a WHERE a.owner=? AND a.id=?", owner, id), &artist, nil)
	if errors.Is(err, sql.ErrNoRows) {
		return artist, domain.Fail("NOT_FOUND", "Music artist not found")
	}
	return artist, err
}

func (s *Store) MusicArtist(ctx context.Context, owner, id string) (music.Artist, error) {
	return readArtistNames(ctx, s.DB, owner, id)
}

func (s *Store) SaveMusicArtistNames(ctx context.Context, owner, id string, revision int64, input music.ArtistNamesInput, key string) (music.Artist, error) {
	names, err := scraper.NormalizeLocalizedTitles(input.LocalizedNames)
	if err != nil || names == nil || id == "" || revision < 1 {
		return music.Artist{}, domain.Fail("INVALID_ARGUMENT", "Invalid localized artist names")
	}
	return saveMusic(ctx, s, owner, "artist-names", id, key, revision, names, func(tx *sql.Tx, artistID string) (music.Artist, error) {
		result, err := tx.ExecContext(ctx, "UPDATE music_artists SET names_revision=names_revision+1 WHERE owner=? AND id=? AND names_revision=?", owner, artistID, revision)
		if err != nil {
			return music.Artist{}, err
		}
		if err = checkMusicUpdate(ctx, tx, result, owner, artistID, "music_artists"); err != nil {
			return music.Artist{}, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM music_artist_localized_names WHERE artist_id=?", artistID); err != nil {
			return music.Artist{}, err
		}
		for _, name := range names {
			if _, err = tx.ExecContext(ctx, "INSERT INTO music_artist_localized_names(artist_id,language,name) VALUES(?,?,?)", artistID, name.Language, name.Title); err != nil {
				return music.Artist{}, err
			}
		}
		return readArtistNames(ctx, tx, owner, artistID)
	})
}
