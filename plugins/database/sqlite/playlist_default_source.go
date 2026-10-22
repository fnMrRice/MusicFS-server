package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

// selectPlaylistVersion 只在当前歌单中未隐藏的多个版本之间选择，不扩大来源集合。
func selectPlaylistVersion(ctx context.Context, tx *sql.Tx, owner string, entry *playlists.AggregateEntry) error {
	if entry.RecordingID == nil || entry.Hidden {
		return nil
	}
	candidates := map[string]bool{}
	for _, contribution := range entry.Provenance {
		if !contribution.Hidden && contribution.Source.SourceTrackID != nil {
			candidates[*contribution.Source.SourceTrackID] = true
		}
	}
	if len(candidates) < 2 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT t.document,d.source_id FROM tracks t
 JOIN recording_source_links l ON l.owner=t.owner AND l.source_id=t.id
 LEFT JOIN recording_default_sources d ON d.owner=l.owner AND d.recording_id=l.recording_id
 WHERE t.owner=? AND l.recording_id=?`, owner, *entry.RecordingID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var selected *library.Track
	var manual *string
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw, &manual); err != nil {
			return err
		}
		var track library.Track
		if err = json.Unmarshal(raw, &track); err != nil {
			return err
		}
		if !candidates[track.ID] {
			continue
		}
		if selected == nil || manual != nil && track.ID == *manual ||
			(manual == nil || selected.ID != *manual) && preferredVersionLess(track, *selected) {
			selected = &track
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if selected != nil {
		entry.Source = playlists.SourceRef{SourceTrackID: &selected.ID}
	}
	return nil
}
