package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

func bumpPersonalRevision(ctx context.Context, tx *sql.Tx, owner, kind string) error {
	_, err := tx.ExecContext(ctx, "INSERT INTO personal_collection_revisions(owner,kind,revision) VALUES(?,?,1) ON CONFLICT(owner,kind) DO UPDATE SET revision=revision+1", owner, kind)
	return err
}

func readPersonalTrack(ctx context.Context, tx *sql.Tx, owner, id string) (*library.Track, error) {
	var track library.Track
	var raw []byte
	err := tx.QueryRowContext(ctx, "SELECT document FROM tracks WHERE owner=? AND id=?", owner, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return &track, domain.Fail("NOT_FOUND", "Track was not found")
	}
	if err != nil {
		return &track, err
	}
	err = json.Unmarshal(raw, &track)
	return &track, err
}

func (s *Store) PersonalCollection(ctx context.Context, owner, kind, cursor string, limit int) (personal.Page, error) {
	page := personal.Page{Items: []personal.Entry{}}
	if !personal.ValidKind(kind) || limit < 1 || limit > 100 {
		return page, personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if kind == personal.Recent {
		if err = pruneRecent(ctx, tx, owner); err != nil {
			return page, err
		}
	}
	var collectionRevision, libraryRevision int64
	if err = tx.QueryRowContext(ctx, "SELECT COALESCE((SELECT revision FROM personal_collection_revisions WHERE owner=? AND kind=?),0),(SELECT revision FROM library_revision)", owner, kind).Scan(&collectionRevision, &libraryRevision); err != nil {
		return page, err
	}
	page.Revision = fmt.Sprintf("%d/%d", collectionRevision, libraryRevision)
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, "DELETE FROM personal_collection_cursors WHERE julianday(expires_at)<=julianday(?)", now.Format(time.RFC3339Nano)); err != nil {
		return page, err
	}
	position := 0
	if cursor != "" {
		err = tx.QueryRowContext(ctx, "SELECT position FROM personal_collection_cursors WHERE id=? AND owner=? AND kind=? AND revision=?", cursor, owner, kind, page.Revision).Scan(&position)
		if errors.Is(err, sql.ErrNoRows) {
			return page, domain.Fail("CURSOR_EXPIRED", "Collection changed or cursor expired; refresh the list")
		}
		if err != nil {
			return page, err
		}
	}
	var tracks []music.Track
	byID := map[string]int{}
	var sources []*string
	var trackPositions []int
	err = musicRows(ctx, tx, `SELECT `+musicTrackColumns+`,p.forced_source_id,p.updated_at FROM music_playlist_entries p JOIN music_tracks m ON m.id=p.track_id AND m.owner=p.owner
 WHERE p.owner=? AND p.playlist_id=? AND p.binding_id='' ORDER BY p.updated_at DESC,p.entry_id LIMIT ? OFFSET ?`, func(rows *sql.Rows) error {
		var source *string
		var stamp string
		track, err := scanMusicTrack(rows, &source, &stamp)
		if err != nil {
			return err
		}
		t, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return err
		}
		index, exists := byID[track.ID]
		if !exists {
			index = len(tracks)
			byID[track.ID] = index
			tracks = append(tracks, track)
		}
		trackPositions = append(trackPositions, index)
		sources = append(sources, source)
		page.Items = append(page.Items, personal.Entry{RecordedAt: t})
		return nil
	}, owner, kind, limit+1, position)
	if err != nil {
		return page, err
	}
	if err = loadMusicRelations(ctx, tx, owner, tracks, false); err != nil {
		return page, err
	}
	for i, pos := range trackPositions {
		t := tracks[pos]
		page.Items[i].ForcedSourceID = sources[i]
		page.Items[i].MusicTrack = t
		page.Items[i].Track = t.SelectedSource
		if sources[i] != nil {
			page.Items[i].Track = exactMusicSource(t, *sources[i])
		}
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		var next string
		err = tx.QueryRowContext(ctx, "SELECT id FROM personal_collection_cursors WHERE owner=? AND kind=? AND revision=? AND position=? LIMIT 1", owner, kind, page.Revision, position+limit).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			var count int
			if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM personal_collection_cursors WHERE owner=?", owner).Scan(&count); err != nil {
				return page, err
			}
			if count >= 2048 {
				return page, domain.Fail("RATE_LIMITED", "Too many active personal collection cursors")
			}
			next = domain.NewID()
			_, err = tx.ExecContext(ctx, "INSERT INTO personal_collection_cursors VALUES(?,?,?,?,?,?)", next, owner, kind, page.Revision, position+limit, now.Add(10*time.Minute).Format(time.RFC3339Nano))
		}
		if err != nil {
			return page, err
		}
		page.NextCursor = &next
	}
	return page, tx.Commit()
}

func (s *Store) PutFavorite(ctx context.Context, owner, id string) (personal.Entry, error) {
	return s.setFavorite(ctx, owner, id, nil, false)
}

func (s *Store) SetFavoriteSource(ctx context.Context, owner, id string, source *string) (personal.Entry, error) {
	return s.setFavorite(ctx, owner, id, source, true)
}

func (s *Store) setFavorite(ctx context.Context, owner, id string, source *string, changeSource bool) (personal.Entry, error) {
	var entry personal.Entry
	if !personal.ValidID(id) {
		return entry, personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return entry, err
	}
	defer tx.Rollback()
	id, err = personalTrackID(ctx, tx, owner, id)
	if err != nil {
		return entry, err
	}
	entry.MusicTrack, err = readMusicTrack(ctx, tx, owner, id)
	if err != nil {
		return entry, err
	}
	entry.Track = entry.MusicTrack.SelectedSource
	var stamp string
	err = tx.QueryRowContext(ctx, "SELECT created_at,forced_source_id FROM music_playlist_entries WHERE owner=? AND playlist_id='favorites' AND binding_id='' AND entry_id=?", owner, id).Scan(&stamp, &entry.ForcedSourceID)
	if errors.Is(err, sql.ErrNoRows) {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM music_playlist_entries WHERE owner=? AND playlist_id='favorites' AND binding_id=''", owner).Scan(&count); err != nil {
			return entry, err
		}
		if count >= personal.MaxFavorites {
			return entry, domain.Fail("RATE_LIMITED", "Favorite collection limit reached")
		}
		stamp = time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
		_, err = tx.ExecContext(ctx, `INSERT INTO music_playlist_entries(owner,playlist_id,binding_id,entry_id,track_id,position,created_at,updated_at) VALUES(?,'favorites','',?,?,0,?,?)`, owner, id, id, stamp, stamp)
		if err != nil {
			return entry, err
		}
		err = bumpPersonalRevision(ctx, tx, owner, personal.Favorites)
	}
	if err != nil {
		return entry, err
	}
	if changeSource && !sameSource(source, entry.ForcedSourceID) {
		if source != nil {
			var member bool
			if err = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM music_sources WHERE owner=? AND track_id=? AND id=?)", owner, id, *source).Scan(&member); err != nil {
				return entry, err
			}
			if !member {
				return entry, domain.Fail("CONFLICT", "Selected source is not a member of the track")
			}
		}
		if _, err = tx.ExecContext(ctx, "UPDATE music_playlist_entries SET forced_source_id=? WHERE owner=? AND playlist_id='favorites' AND binding_id='' AND entry_id=?", source, owner, id); err != nil {
			return entry, err
		}
		entry.ForcedSourceID = source
		if err = bumpPersonalRevision(ctx, tx, owner, personal.Favorites); err != nil {
			return entry, err
		}
	}
	if entry.ForcedSourceID != nil {
		entry.Track = exactMusicSource(entry.MusicTrack, *entry.ForcedSourceID)
	}
	entry.RecordedAt, err = time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return entry, err
	}
	return entry, tx.Commit()
}

func (s *Store) DeletePersonalCollection(ctx context.Context, owner, kind, id string) error {
	if !personal.ValidKind(kind) || id != "" && !personal.ValidID(id) {
		return personal.Invalid()
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if kind == personal.Favorites && id != "" {
		id, err = personalTrackID(ctx, tx, owner, id)
		if err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM music_playlist_entries WHERE owner=? AND playlist_id=? AND binding_id='' AND (?='' OR entry_id=?)", owner, kind, id, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		if err = bumpPersonalRevision(ctx, tx, owner, kind); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM personal_collection_cursors WHERE owner=? AND kind=?", owner, kind); err != nil {
		return err
	}
	if kind == personal.Recent {
		if _, err = tx.ExecContext(ctx, "DELETE FROM personal_playback_receipts WHERE owner=? AND history_target IS NULL AND (?='' OR source_track_id=?)", owner, id, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func personalTrackID(ctx context.Context, tx *sql.Tx, owner, id string) (string, error) {
	var resolved string
	err := tx.QueryRowContext(ctx, "SELECT coalesce((SELECT track_id FROM music_sources WHERE owner=? AND id=?),?)", owner, id, id).Scan(&resolved)
	return resolved, err
}

func sameSource(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func exactMusicSource(track music.Track, id string) *library.Track {
	if track.SelectedSource == nil {
		return nil
	}
	for i := range track.SelectedSource.Versions {
		member := &track.SelectedSource.Versions[i]
		if member.ID == id {
			return member
		}
	}
	return nil
}
