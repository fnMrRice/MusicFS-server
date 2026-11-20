package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// 资料、关系、曲库修订和幂等回执在同一事务中发布。
func saveMusic[T any](ctx context.Context, s *Store, owner, kind, id, key string, revision int64, input any, save func(*sql.Tx, string) (T, error)) (T, error) {
	var res T
	if !works.ValidID(key) || len(key) < 8 || !works.ValidID(owner) || id != "" && !works.ValidID(id) || id == "" && revision != 0 || id != "" && revision < 1 {
		return res, domain.Fail("INVALID_ARGUMENT", "Invalid music metadata mutation")
	}
	route := fmt.Sprintf("music/%s/%s", kind, id)
	digest, err := writebackDigest(struct {
		Revision int64
		Input    any
	}{revision, input})
	if err != nil {
		return res, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	prior, exists, err := contentIdempotency(ctx, tx, owner, route, key, digest)
	if err != nil {
		return res, err
	}
	if exists {
		err = json.Unmarshal(prior, &res)
		return res, err
	}
	if id == "" {
		id = domain.NewID()
	}
	res, err = save(tx, id)
	if err != nil {
		return res, err
	}
	changed := true
	if track, ok := any(res).(music.Track); kind == "tracks" && ok && track.Revision == revision {
		changed = false
	}
	if resultRevision, ok := any(res).(int64); ok && resultRevision == revision {
		changed = false
	}
	if changed {
		_, err = tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1")
	}
	if err != nil {
		return res, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return res, err
	}
	if err = saveContentIdempotency(ctx, tx, owner, route, key, digest, raw); err != nil {
		return res, err
	}
	return res, tx.Commit()
}

func checkMusicUpdate(ctx context.Context, tx *sql.Tx, result sql.Result, owner, id, table string) error {
	n, err := result.RowsAffected()
	if err != nil || n > 0 {
		return err
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE owner=? AND id=?)", owner, id).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return domain.Fail("NOT_FOUND", "Music library item not found")
	}
	return domain.Fail("CONFLICT", "Music metadata revision changed")
}

func (s *Store) SaveMusicAlbum(ctx context.Context, owner, id string, revision int64, input music.AlbumFields, key string) (music.Album, error) {
	a, err := input.Normalize()
	if err != nil {
		return music.Album{}, err
	}
	return saveMusic(ctx, s, owner, "albums", id, key, revision, a, func(tx *sql.Tx, newID string) (music.Album, error) {
		links, err := json.Marshal(a.SourceLinks)
		if err != nil {
			return music.Album{}, err
		}
		if id == "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO music_albums(id,owner,name,external_id,is_external,cover_path,publisher,release_date,source_links,origin,revision,metadata_locked)
 VALUES(?,?,?,?,?,?,?,?,?,'manual',1,1)`, newID, owner, a.Name, a.ExternalID, a.IsExternal, a.CoverPath, a.Publisher, a.ReleaseDate, string(links))
		} else {
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE music_albums SET name=?,external_id=?,is_external=CASE WHEN origin='cd' THEN 1 ELSE ? END,cover_path=?,publisher=?,release_date=?,source_links=?,revision=revision+1,metadata_locked=1 WHERE owner=? AND id=? AND revision=?`,
				a.Name, a.ExternalID, a.IsExternal, a.CoverPath, a.Publisher, a.ReleaseDate, string(links), owner, newID, revision)
			if err == nil {
				err = checkMusicUpdate(ctx, tx, result, owner, newID, "music_albums")
			}
		}
		if err != nil {
			return music.Album{}, err
		}
		return readMusicAlbum(ctx, tx, owner, newID)
	})
}

func (s *Store) SaveMusicTrack(ctx context.Context, owner, id string, revision int64, input music.TrackInput, key string) (music.Track, error) {
	t, err := input.Normalize()
	if err != nil {
		return music.Track{}, err
	}
	var titles *string
	if t.LocalizedTitles != nil {
		raw, err := json.Marshal(t.LocalizedTitles)
		if err != nil {
			return music.Track{}, err
		}
		value := string(raw)
		titles = &value
	}
	return saveMusic(ctx, s, owner, "tracks", id, key, revision, t, func(tx *sql.Tx, newID string) (music.Track, error) {
		var err error
		if id == "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO music_tracks(id,owner,title,localized_titles,cover_path,genre,year,rating,origin,revision,metadata_locked)
 VALUES(?,?,?,coalesce(?,'[]'),?,?,?,?,'manual',1,1)`, newID, owner, t.Title, titles, t.CoverPath, t.Genre, t.Year, t.Rating)
			if err == nil {
				now := time.Now().UTC().Format(time.RFC3339Nano)
				_, err = tx.ExecContext(ctx, "INSERT INTO recordings(id,owner,created_at,updated_at) VALUES(?,?,?,?)", newID, owner, now, now)
			}
		} else {
			if err := checkMusicTrackIdentity(ctx, tx, owner, newID); err != nil {
				return music.Track{}, err
			}
			var same bool
			same, err = unchangedMusicTrack(ctx, tx, owner, newID, revision, t, titles)
			if err != nil {
				return music.Track{}, err
			}
			if same {
				return readMusicTrack(ctx, tx, owner, newID)
			}
			var result sql.Result
			result, err = tx.ExecContext(ctx, `UPDATE music_tracks SET title=?,localized_titles=coalesce(?,localized_titles),cover_path=?,genre=?,year=?,rating=?,
 revision=revision+1,metadata_locked=1,scraper_placeholder=0 WHERE owner=? AND id=? AND revision=?`,
				t.Title, titles, t.CoverPath, t.Genre, t.Year, t.Rating, owner, newID, revision)
			if err == nil {
				err = checkMusicUpdate(ctx, tx, result, owner, newID, "music_tracks")
			}
		}
		if err != nil {
			return music.Track{}, err
		}
		if err = writeMusicCredits(ctx, tx, owner, newID, t.ArtistNames); err != nil {
			return music.Track{}, err
		}
		if t.AlbumIDs != nil {
			if err = writeMusicAlbumLinks(ctx, tx, owner, newID, t.AlbumIDs); err != nil {
				return music.Track{}, err
			}
		}
		return readMusicTrack(ctx, tx, owner, newID)
	})
}

// 同一资料的重复保存不重排歌手、专辑及时间。首次锁定扫描资料仍算一次编辑。
func unchangedMusicTrack(ctx context.Context, tx *sql.Tx, owner, id string, revision int64, input music.TrackInput, titles *string) (bool, error) {
	artists, _ := json.Marshal(input.ArtistNames)
	albums, _ := json.Marshal(input.AlbumIDs)
	var identical bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM music_tracks WHERE owner=? AND id=? AND revision=? AND metadata_locked=1
 AND title=? AND localized_titles=coalesce(?,localized_titles) AND cover_path IS ? AND genre IS ? AND year IS ? AND rating IS ?
 AND NOT EXISTS(SELECT a.name,x.position FROM music_track_artists x JOIN music_artists a ON a.id=x.artist_id WHERE x.track_id=? EXCEPT SELECT value,key FROM json_each(?))
 AND (SELECT count(*) FROM music_track_artists WHERE track_id=?)=json_array_length(?)
 AND (? OR ((SELECT count(*) FROM music_manual_album_tracks WHERE track_id=?)=json_array_length(?) AND NOT EXISTS(
   SELECT album_id FROM music_manual_album_tracks WHERE track_id=? EXCEPT SELECT value FROM json_each(?)))))`, owner, id, revision, input.Title, titles, input.CoverPath, input.Genre, input.Year, input.Rating, id, string(artists), id, string(artists), input.AlbumIDs == nil, id, string(albums), id, string(albums)).Scan(&identical)
	return identical, err
}

func writeMusicCredits(ctx context.Context, tx *sql.Tx, owner, id string, names []string) error {
	if _, err := tx.ExecContext(ctx, "DELETE FROM music_track_artists WHERE track_id=?", id); err != nil {
		return err
	}
	for order, name := range names {
		var artist string
		err := tx.QueryRowContext(ctx, "SELECT id FROM music_artists WHERE owner=? AND name=?", owner, name).Scan(&artist)
		if errors.Is(err, sql.ErrNoRows) {
			artist = domain.NewID()
			_, err = tx.ExecContext(ctx, "INSERT INTO music_artists(id,owner,name) VALUES(?,?,?)", artist, owner, name)
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO music_track_artists(track_id,artist_id,position) VALUES(?,?,?)", id, artist, order); err != nil {
			return err
		}
	}
	return nil
}

func writeMusicAlbumLinks(ctx context.Context, tx *sql.Tx, owner, id string, albums []string) error {
	raw, _ := json.Marshal(albums)
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_manual_album_tracks WHERE track_id=? AND album_id NOT IN(SELECT value FROM json_each(?))`, id, string(raw)); err != nil {
		return err
	}
	for _, album := range albums {
		result, err := tx.ExecContext(ctx, `INSERT INTO music_manual_album_tracks(album_id,track_id,position)
 SELECT a.id,?,coalesce((SELECT max(position)+1 FROM music_album_tracks x WHERE x.album_id=a.id),0)
 FROM music_albums a WHERE a.owner=? AND a.id=? ON CONFLICT(album_id,track_id) DO UPDATE SET track_id=excluded.track_id`, id, owner, album)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return domain.Fail("NOT_FOUND", "Music album not found")
		}
	}
	return nil
}
