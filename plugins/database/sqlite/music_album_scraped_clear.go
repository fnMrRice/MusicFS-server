package sqlite

import (
	"context"
	"database/sql"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

// ClearMusicAlbumScrapedFields 清理一张专辑的刮削快照和已无用途的占位，已合入曲库的主资料继续保留。
func (s *Store) ClearMusicAlbumScrapedFields(ctx context.Context, owner, id string, revision int64, key string) (int64, error) {
	if !works.ValidID(id) {
		return 0, domain.Fail("INVALID_ARGUMENT", "Album is required")
	}
	return saveMusic(ctx, s, owner, "albums/scraped-clear", id, key, revision, nil, func(tx *sql.Tx, albumID string) (int64, error) {
		album, err := readMusicAlbum(ctx, tx, owner, albumID)
		if err != nil {
			return 0, err
		}
		if album.Revision != revision {
			return 0, domain.Fail("CONFLICT", "Music album revision changed")
		}
		// 更换发行版后，旧占位可能只剩专辑成员关系。按成员和占位标识判断，而非仅查看最新曲目表。
		var deleted []string
		err = musicRows(ctx, tx, `DELETE FROM music_tracks WHERE id IN (
 SELECT m.id FROM music_manual_album_tracks x JOIN music_tracks m ON m.id=x.track_id
 WHERE x.album_id=? AND m.owner=? AND `+disposableAlbumPlaceholder+`) RETURNING id`, func(rows *sql.Rows) error {
			var trackID string
			if err := rows.Scan(&trackID); err != nil {
				return err
			}
			deleted = append(deleted, trackID)
			return nil
		}, albumID, owner)
		if err != nil {
			return 0, err
		}
		for _, trackID := range deleted {
			if err := deleteUnusedMusicRecording(ctx, tx, owner, trackID); err != nil {
				return 0, err
			}
		}
		changed := len(deleted) > 0
		for _, query := range []string{
			"DELETE FROM music_album_scraped_track_refs WHERE album_id=?",
			"DELETE FROM music_album_scraped_fields WHERE album_id=?",
		} {
			result, err := tx.ExecContext(ctx, query, albumID)
			if err != nil {
				return 0, err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return 0, err
			}
			changed = changed || n > 0
		}
		if !changed {
			return revision, nil
		}
		result, err := tx.ExecContext(ctx, "UPDATE music_albums SET revision=revision+1 WHERE owner=? AND id=? AND revision=?", owner, albumID, revision)
		if err != nil {
			return 0, err
		}
		if err := checkMusicUpdate(ctx, tx, result, owner, albumID, "music_albums"); err != nil {
			return 0, err
		}
		return revision + 1, nil
	})
}
