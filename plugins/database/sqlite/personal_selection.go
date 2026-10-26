package sqlite

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

// SetFavoritesSelection 在同一事务内展开选择、预检限额并更新收藏，不改动已有收藏时间。
func (s *Store) SetFavoritesSelection(ctx context.Context, owner string, selection library.TrackSelection, favorite bool) (personal.SelectionResult, error) {
	result := personal.SelectionResult{SourceTrackIDs: []string{}}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	tracks, err := resolveTrackSelection(ctx, tx, owner, selection)
	if err != nil {
		return result, err
	}
	if len(tracks) > personal.MaxFavorites {
		return result, domain.Fail("RATE_LIMITED", "Favorite selection limit reached")
	}
	rows, err := tx.QueryContext(ctx, "SELECT source_track_id FROM personal_favorites WHERE owner=?", owner)
	if err != nil {
		return result, err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return result, err
		}
		existing[id] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	missing := 0
	for _, track := range tracks {
		result.SourceTrackIDs = append(result.SourceTrackIDs, track.ID)
		if !existing[track.ID] {
			missing++
		}
	}
	if favorite && len(existing)+missing > personal.MaxFavorites {
		return result, domain.Fail("RATE_LIMITED", "Favorite collection limit reached")
	}
	query := "DELETE FROM personal_favorites WHERE owner=? AND source_track_id=?"
	if favorite {
		query = "INSERT INTO personal_favorites(owner,source_track_id,created_at) VALUES(?,?,?) ON CONFLICT(owner,source_track_id) DO NOTHING"
	}
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return result, err
	}
	defer stmt.Close()
	stamp := time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")
	for _, id := range result.SourceTrackIDs {
		if existing[id] == favorite {
			continue
		}
		args := []any{owner, id}
		if favorite {
			args = append(args, stamp)
		}
		if _, err = stmt.ExecContext(ctx, args...); err != nil {
			return result, err
		}
		result.Affected++
	}
	if result.Affected > 0 {
		if err = bumpPersonalRevision(ctx, tx, owner, personal.Favorites); err != nil {
			return result, err
		}
	}
	return result, tx.Commit()
}
