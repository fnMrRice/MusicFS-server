package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

// 先确定音乐库可见的来源，再按规范曲目聚合。无来源的手工条目保留给分类及资料查询。
const musicVisible = ordinaryMusicSources + `, visible_music_sources AS (
 SELECT s.id,s.track_id,s.provider_instance_id FROM music_sources s JOIN tracks t ON t.id=s.id AND t.owner=s.owner
 WHERE s.owner=? AND ` + ordinaryMusicTrack + `
), visible_music_tracks AS (
 SELECT m.id FROM music_tracks m WHERE m.owner=? AND (
 EXISTS (SELECT 1 FROM visible_music_sources s WHERE s.track_id=m.id)
 OR ((m.origin='manual' OR (m.origin='recording' AND m.metadata_locked=1)) AND NOT EXISTS(SELECT 1 FROM music_sources s WHERE s.track_id=m.id)))
) `

const musicTrackColumns = "m.id,m.title,m.localized_titles,m.cover_path,m.genre,m.year,m.rating,m.revision,m.created_at,m.updated_at"

type musicScanner interface{ Scan(...any) error }

func scanMusicTrack(row musicScanner, extra ...any) (music.Track, error) {
	t := music.Track{Artists: []music.Artist{}, Albums: []music.Album{}, AlbumIDs: []string{}, Sources: []music.Source{}}
	var titles []byte
	fields := []any{&t.ID, &t.Title, &titles, &t.CoverPath, &t.Genre, &t.Year, &t.Rating, &t.Revision, &t.CreatedAt, &t.UpdatedAt}
	if err := row.Scan(append(fields, extra...)...); err != nil {
		return t, err
	}
	err := json.Unmarshal(titles, &t.LocalizedTitles)
	return t, err
}

// 游标绑定账号、完整查询条件和两种修订，资料或成员变化后提示从头读取。
func musicCursorScope(owner string, q music.Query, revision string, matching int64) string {
	q.After = ""
	q.Limit = 0
	raw, _ := json.Marshal([]any{owner, q, revision, matching})
	digest := sha256.Sum256(raw)
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

func musicCursorAfter(cursor, scope string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	id, stamp, ok := strings.Cut(cursor, ".")
	if ok && stamp == scope {
		raw, err := base64.RawURLEncoding.DecodeString(id)
		if err == nil && len(raw) > 0 && len(raw) <= 128 {
			return string(raw), nil
		}
	}
	return "", domain.Fail("CURSOR_EXPIRED", "Music library changed or cursor does not match the query")
}

func (s *Store) MusicTracks(ctx context.Context, owner string, q music.Query) (music.TrackPage, error) {
	page := music.TrackPage{Items: []music.Track{}}
	if err := q.Validate(); err != nil {
		return page, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	var revision int64
	if err = tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&revision); err != nil {
		return page, err
	}
	page.LibraryRevision = fmt.Sprint(revision)
	if page.MatchingRevision, err = matchingRevision(ctx, tx, owner); err != nil {
		return page, err
	}
	scope := musicCursorScope(owner, q, page.LibraryRevision, page.MatchingRevision)
	after, err := musicCursorAfter(q.After, scope)
	if err != nil {
		return page, err
	}
	position := `(SELECT position FROM music_album_tracks WHERE album_id=? AND track_id=m.id)`
	order, cursor := "m.title COLLATE NOCASE,m.id", "(?='' OR (m.title COLLATE NOCASE,m.id)>(SELECT title COLLATE NOCASE,id FROM music_tracks WHERE owner=? AND id=?))"
	args := []any{owner, owner, owner, after, owner, after}
	if q.AlbumID != "" {
		order = position + ",m.id"
		cursor = "(?='' OR (" + position + ",m.id)>(SELECT position,track_id FROM music_album_tracks WHERE album_id=? AND track_id=?))"
		args = []any{owner, owner, owner, after, q.AlbumID, q.AlbumID, after}
	}
	args = append(args, q.SourceOnly, q.ProviderID, q.ProviderID, q.ArtistID, q.ArtistID, q.AlbumID, q.AlbumID, q.Genre, q.Genre, q.Unknown, q.Unknown, q.Unknown, q.Unknown, q.Q, q.Q, q.Q, q.Q, q.Q, q.Q)
	if q.AlbumID != "" {
		args = append(args, q.AlbumID)
	}
	args = append(args, q.Limit+1)
	rows, err := tx.QueryContext(ctx, musicVisible+`SELECT `+musicTrackColumns+` FROM music_tracks m JOIN visible_music_tracks v ON v.id=m.id
 WHERE `+cursor+`
 AND (?=0 OR EXISTS(SELECT 1 FROM visible_music_sources s WHERE s.track_id=m.id))
 AND (?='' OR EXISTS(SELECT 1 FROM visible_music_sources s WHERE s.track_id=m.id AND s.provider_instance_id=?))
 AND (?='' OR EXISTS(SELECT 1 FROM music_track_artists a WHERE a.track_id=m.id AND a.artist_id=?))
 AND (?='' OR EXISTS(SELECT 1 FROM music_album_tracks x JOIN music_albums a ON a.id=x.album_id WHERE x.track_id=m.id AND a.id=? AND `+ordinaryMusicAlbumMetadata+`))
 AND (?='' OR lower(m.genre)=lower(?)) AND (?='' OR
 (?='genre' AND coalesce(m.genre,'')='') OR
 (?='artist' AND NOT EXISTS(SELECT 1 FROM music_track_artists x WHERE x.track_id=m.id)) OR
 (?='album' AND NOT EXISTS(SELECT 1 FROM music_album_tracks x JOIN music_albums a ON a.id=x.album_id WHERE x.track_id=m.id AND `+ordinaryMusicAlbumMetadata+`)))
 AND (?='' OR instr(lower(m.title),lower(?))>0
 OR EXISTS(SELECT 1 FROM json_each(m.localized_titles) t WHERE instr(lower(json_extract(t.value,'$.title')),lower(?))>0)
 OR EXISTS(SELECT 1 FROM music_track_artists x JOIN music_artists a ON x.artist_id=a.id WHERE x.track_id=m.id AND instr(lower(a.name),lower(?))>0)
 OR EXISTS(SELECT 1 FROM music_track_artists x JOIN music_artist_localized_names n ON n.artist_id=x.artist_id WHERE x.track_id=m.id AND instr(lower(n.name),lower(?))>0)
 OR EXISTS(SELECT 1 FROM music_album_tracks x JOIN music_albums a ON x.album_id=a.id WHERE x.track_id=m.id AND `+ordinaryMusicAlbumMetadata+` AND instr(lower(a.name),lower(?))>0))
 ORDER BY `+order+` LIMIT ?`, args...)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		t, e := scanMusicTrack(rows)
		if e != nil {
			rows.Close()
			return page, e
		}
		page.Items = append(page.Items, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		next := base64.RawURLEncoding.EncodeToString([]byte(page.Items[q.Limit-1].ID)) + "." + scope
		page.NextAfter = &next
	}
	if err = loadMusicRelations(ctx, tx, owner, page.Items, true, albumContext{q.AlbumID, q.ProviderID}); err != nil {
		return page, err
	}
	return page, tx.Commit()
}

func readMusicTrack(ctx context.Context, tx *sql.Tx, owner, id string) (music.Track, error) {
	return readMusicTrackInScope(ctx, tx, owner, id, albumContext{})
}

func readMusicTrackInScope(ctx context.Context, tx *sql.Tx, owner, id string, scope albumContext) (music.Track, error) {
	t, err := scanMusicTrack(tx.QueryRowContext(ctx, "SELECT "+musicTrackColumns+" FROM music_tracks m WHERE m.owner=? AND m.id=?", owner, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, domain.Fail("NOT_FOUND", "Music track not found")
	}
	if err != nil {
		return t, err
	}
	items := []music.Track{t}
	err = loadMusicRelations(ctx, tx, owner, items, false, scope)
	return items[0], err
}

func (s *Store) MusicTrack(ctx context.Context, owner, id string) (music.Track, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return music.Track{}, err
	}
	defer tx.Rollback()
	if err := checkMusicTrackIdentity(ctx, tx, owner, id); err != nil {
		return music.Track{}, err
	}
	t, err := readMusicTrack(ctx, tx, owner, id)
	if err != nil {
		return t, err
	}
	return t, tx.Commit()
}

// 单文件归并后仍保留旧资料供解绑恢复。编辑器须重新选择当前曲目才能继续读写。
func checkMusicTrackIdentity(ctx context.Context, tx *sql.Tx, owner, id string) error {
	var moved bool
	if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM music_sources WHERE owner=? AND id=? AND track_id<>id)", owner, id).Scan(&moved); err != nil {
		return err
	}
	if moved {
		return domain.Fail("RESOURCE_CHANGED", "Track source now belongs to a different recording")
	}
	return nil
}

func (s *Store) MusicTrackForSource(ctx context.Context, owner, source string) (music.Track, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return music.Track{}, err
	}
	defer tx.Rollback()
	var id, provider string
	err = tx.QueryRowContext(ctx, "SELECT track_id,provider_instance_id FROM music_sources WHERE owner=? AND id=?", owner, source).Scan(&id, &provider)
	if errors.Is(err, sql.ErrNoRows) {
		return music.Track{}, domain.Fail("NOT_FOUND", "Music source not found")
	}
	if err != nil {
		return music.Track{}, err
	}
	track, err := readMusicTrackInScope(ctx, tx, owner, id, albumContext{providerID: provider})
	if err != nil {
		return track, err
	}
	return track, tx.Commit()
}
