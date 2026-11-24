package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
)

const ordinaryMusicAlbumMetadata = `(a.origin<>'cd' OR EXISTS(SELECT 1 FROM works w WHERE w.id=a.id AND w.owner=a.owner AND w.show_in_track_list=1))`

const musicCategoryScope = musicVisible + `, scope AS (
 SELECT v.id FROM visible_music_tracks v WHERE (?='' OR EXISTS (
 SELECT 1 FROM visible_music_sources s WHERE s.track_id=v.id AND s.provider_instance_id=?))
) `

func missingMusic(ctx context.Context, tx *sql.Tx, owner string, q music.Query, condition string) (int, error) {
	if q.Q != "" {
		return 0, nil
	}
	var n int
	err := tx.QueryRowContext(ctx, musicCategoryScope+`SELECT count(*) FROM scope JOIN music_tracks m ON m.id=scope.id WHERE `+condition, owner, owner, owner, q.ProviderID, q.ProviderID).Scan(&n)
	return n, err
}

func (s *Store) MusicArtists(ctx context.Context, owner string, q music.Query) (music.ArtistPage, error) {
	res := music.ArtistPage{Items: []music.ArtistItem{}}
	if err := q.Validate(); err != nil {
		return res, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	err = musicRows(ctx, tx, musicCategoryScope+`SELECT `+artistNameColumns+`,count(DISTINCT x.track_id) FROM music_artists a
 JOIN music_track_artists x ON x.artist_id=a.id JOIN scope ON scope.id=x.track_id
 WHERE a.owner=? AND a.id>? AND (instr(lower(a.name),lower(?))>0 OR EXISTS (
 SELECT 1 FROM music_artist_localized_names n WHERE n.artist_id=a.id AND instr(lower(n.name),lower(?))>0))
 GROUP BY a.id ORDER BY a.id LIMIT ?`, func(r *sql.Rows) error {
		var a music.ArtistItem
		if e := scanArtistNames(r, &a.Artist, nil, &a.TrackCount); e != nil {
			return e
		}
		res.Items = append(res.Items, a)
		return nil
	}, owner, owner, owner, q.ProviderID, q.ProviderID, owner, q.After, q.Q, q.Q, q.Limit+1)
	if err != nil {
		return res, err
	}
	n, err := missingMusic(ctx, tx, owner, q, "NOT EXISTS(SELECT 1 FROM music_track_artists x WHERE x.track_id=m.id)")
	if err != nil {
		return res, err
	}
	if n > 0 {
		res.Items = append(res.Items, music.ArtistItem{TrackCount: n})
	}
	if len(res.Items) > q.Limit {
		res.Items = res.Items[:q.Limit]
		res.NextAfter = &res.Items[q.Limit-1].ID
	}
	return res, tx.Commit()
}

func (s *Store) MusicAlbums(ctx context.Context, owner string, q music.Query) (music.AlbumPage, error) {
	res := music.AlbumPage{Items: []music.AlbumItem{}}
	if err := q.Validate(); err != nil {
		return res, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	err = musicRows(ctx, tx, musicCategoryScope+`SELECT `+albumColumns+`,COUNT(DISTINCT scope.id) FROM music_albums a
 LEFT JOIN music_album_tracks x ON x.album_id=a.id LEFT JOIN scope ON scope.id=x.track_id
 WHERE a.owner=? AND a.id>? AND instr(lower(a.name),lower(?))>0 AND `+ordinaryMusicAlbumMetadata+`
 AND (?=0 OR NOT EXISTS(SELECT 1 FROM music_album_scraped_fields f WHERE f.album_id=a.id))
 GROUP BY a.id HAVING COUNT(scope.id)>0 OR (?='' AND a.origin='manual' AND COUNT(x.track_id)=0)
 ORDER BY a.id LIMIT ?`, func(r *sql.Rows) error {
		var a music.AlbumItem
		if e := r.Scan(append(albumFields(&a.Album), &a.TrackCount)...); e != nil {
			return e
		}
		res.Items = append(res.Items, a)
		return nil
	}, owner, owner, owner, q.ProviderID, q.ProviderID, owner, q.After, q.Q, q.UnscrapedOnly, q.ProviderID, q.Limit+1)
	if err != nil {
		return res, err
	}
	if !q.UnscrapedOnly {
		n, err := missingMusic(ctx, tx, owner, q, "NOT EXISTS(SELECT 1 FROM music_album_tracks x JOIN music_albums a ON a.id=x.album_id WHERE x.track_id=m.id AND "+ordinaryMusicAlbumMetadata+")")
		if err != nil {
			return res, err
		}
		if n > 0 {
			res.Items = append(res.Items, music.AlbumItem{Album: music.Album{AlbumFields: music.AlbumFields{SourceLinks: []music.AlbumSourceLink{}}}, TrackCount: n})
		}
	}
	if len(res.Items) > q.Limit {
		res.Items = res.Items[:q.Limit]
		res.NextAfter = &res.Items[q.Limit-1].ID
	}
	albums := make([]*music.Album, len(res.Items))
	for i := range res.Items {
		albums[i] = &res.Items[i].Album
	}
	if err = populateMusicAlbumCovers(ctx, tx, owner, albums...); err != nil {
		return res, err
	}
	return res, tx.Commit()
}

func readMusicAlbum(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, owner, id string) (music.Album, error) {
	var a music.Album
	err := q.QueryRowContext(ctx, "SELECT "+albumColumns+" FROM music_albums a WHERE a.owner=? AND a.id=?", owner, id).Scan(albumFields(&a)...)
	if errors.Is(err, sql.ErrNoRows) {
		return a, domain.Fail("NOT_FOUND", "Music album not found")
	}
	return a, err
}

func (s *Store) MusicAlbum(ctx context.Context, owner, id string) (music.Album, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return music.Album{}, err
	}
	defer tx.Rollback()
	album, err := readMusicAlbum(ctx, tx, owner, id)
	if err != nil {
		return album, err
	}
	if err = populateMusicAlbumCovers(ctx, tx, owner, &album); err != nil {
		return album, err
	}
	return album, tx.Commit()
}

func (s *Store) MusicGenres(ctx context.Context, owner string, q music.Query) (music.GenrePage, error) {
	res := music.GenrePage{Items: []music.GenreItem{}}
	if err := q.Validate(); err != nil {
		return res, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	err = musicRows(ctx, tx, musicCategoryScope+`SELECT min(m.genre),COUNT(*) FROM music_tracks m JOIN scope ON scope.id=m.id
 WHERE coalesce(m.genre,'')<>'' AND m.genre COLLATE NOCASE>? COLLATE NOCASE AND instr(lower(m.genre),lower(?))>0
 GROUP BY m.genre COLLATE NOCASE ORDER BY m.genre COLLATE NOCASE LIMIT ?`, func(r *sql.Rows) error {
		var item music.GenreItem
		if e := r.Scan(&item.Name, &item.TrackCount); e != nil {
			return e
		}
		res.Items = append(res.Items, item)
		return nil
	}, owner, owner, owner, q.ProviderID, q.ProviderID, q.After, q.Q, q.Limit+1)
	if err != nil {
		return res, err
	}
	n, err := missingMusic(ctx, tx, owner, q, "coalesce(m.genre,'')=''")
	if err != nil {
		return res, err
	}
	if n > 0 {
		res.Items = append(res.Items, music.GenreItem{TrackCount: n})
	}
	if len(res.Items) > q.Limit {
		res.Items = res.Items[:q.Limit]
		res.NextAfter = &res.Items[q.Limit-1].Name
	}
	return res, tx.Commit()
}
