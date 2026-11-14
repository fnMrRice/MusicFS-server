package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library/personal"
)

// 优先匹配已导入音轨的显式来源。同路径的扫描副本归入同一作品；
// 有多个候选时按声明顺序和稳定的作品编号选择。
func separateHistoryTarget(ctx context.Context, tx *sql.Tx, owner, sourceTrackID string, allowed []string) (*personal.HistoryTarget, error) {
	if len(allowed) == 0 {
		return nil, nil
	}
	types, err := json.Marshal(allowed)
	if err != nil {
		return nil, err
	}
	target := personal.HistoryTarget{SourceTrackID: sourceTrackID}
	err = tx.QueryRowContext(ctx, `SELECT w.content_type,w.id,w.title,m.title
		FROM tracks t JOIN music_sources s ON s.owner=t.owner AND s.id=t.id
		JOIN music_tracks m ON m.owner=s.owner AND m.id=s.track_id
		JOIN work_packages p ON p.owner=t.owner JOIN works w ON w.owner=p.owner AND w.id=p.work_id
		JOIN json_each(?) allowed ON allowed.value=w.content_type
		JOIN json_each(p.document,'$.nodes') n
		WHERE t.owner=? AND t.id=? AND json_extract(n.value,'$.kind')='audio'
		AND (json_extract(n.value,'$.sourceTrackId')=t.id OR
		 (p.provider_id=t.provider AND p.root_id=t.root_id AND json_extract(n.value,'$.sourcePath')=t.path))
		ORDER BY (json_extract(n.value,'$.sourceTrackId')=t.id) DESC,allowed.key,w.id LIMIT 1`,
		string(types), owner, sourceTrackID).Scan(&target.TypeID, &target.WorkID, &target.WorkTitle, &target.TrackTitle)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &target, err
}

func queueContentHistory(ctx context.Context, tx *sql.Tx, owner, eventID string, target personal.HistoryTarget, stamp string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO content_history_pending(owner,type_id,event_id,work_id,work_title,source_id,track_title,recorded_at)
		VALUES(?,?,?,?,?,?,?,?)`, owner, target.TypeID, eventID, target.WorkID, target.WorkTitle, target.SourceTrackID, target.TrackTitle, stamp)
	return err
}

func (s *Store) PendingContentHistory(ctx context.Context, owner, id string) ([]personal.HistoryDelivery, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT owner,event_id,work_id,work_title,source_id,track_title,recorded_at
		FROM content_history_pending WHERE type_id=? AND (?='' OR owner=?) ORDER BY owner,recorded_at,event_id LIMIT 64`, id, owner, owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []personal.HistoryDelivery{}
	for rows.Next() {
		var item personal.HistoryDelivery
		var stamp string
		if err := rows.Scan(&item.Owner, &item.EventID, &item.Entry.WorkID, &item.Entry.WorkTitle, &item.Entry.SourceTrackID, &item.Entry.TrackTitle, &stamp); err != nil {
			return nil, err
		}
		item.Entry.RecordedAt, err = time.Parse(time.RFC3339Nano, stamp)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) ConfirmContentHistory(ctx context.Context, owner, id, eventID string) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM content_history_pending WHERE owner=? AND type_id=? AND event_id=?", owner, id, eventID)
	return err
}
