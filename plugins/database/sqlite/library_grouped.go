package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

type groupedRevision struct {
	Library  string `json:"library"`
	Matching int64  `json:"matching"`
}

// ListGroupedTracks 先固定完整录音成员再分页，避免同一录音的版本被原始曲目页截断。
// 复用持久分页表，但以独立作用域绑定游标，不能与普通曲目列表混用。
func (s *Store) ListGroupedTracks(ctx context.Context, owner, provider, cursor string, limit int) (library.Page, error) {
	page := library.Page{Items: []library.Track{}}
	if owner == "" || limit < 1 || limit > 200 {
		return page, domain.Fail("INVALID_ARGUMENT", "Invalid grouped library request")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	if _, err = tx.ExecContext(ctx, "DELETE FROM library_snapshots WHERE julianday(expires_at)<julianday(?)", now.Format(time.RFC3339Nano)); err != nil {
		return page, err
	}
	scope := "grouped-recording:" + provider
	var snapshot, rawRevision string
	var position int
	var revision groupedRevision
	if cursor != "" {
		err = tx.QueryRowContext(ctx, `SELECT s.id,s.revision,c.position FROM library_cursors c
 JOIN library_snapshots s ON s.id=c.snapshot_id WHERE c.id=? AND s.owner=? AND s.provider=?`, cursor, owner, scope).Scan(&snapshot, &rawRevision, &position)
		if errors.Is(err, sql.ErrNoRows) {
			return page, domain.Fail("CURSOR_EXPIRED", "Cursor is expired or does not match this request")
		}
		if err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(rawRevision), &revision); err != nil {
			return page, err
		}
	} else {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM library_snapshots WHERE owner=?", owner).Scan(&count); err != nil {
			return page, err
		}
		if count >= 64 {
			return page, domain.Fail("RATE_LIMITED", "Too many active list snapshots")
		}
		var value int64
		if err = tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&value); err != nil {
			return page, err
		}
		revision.Library = fmt.Sprint(value)
		if revision.Matching, err = matchingRevision(ctx, tx, owner); err != nil {
			return page, err
		}
		items, err := groupedLibraryItems(ctx, tx, owner, provider)
		if err != nil {
			return page, err
		}
		snapshot = domain.NewID()
		raw, _ := json.Marshal(revision)
		if _, err = tx.ExecContext(ctx, "INSERT INTO library_snapshots VALUES(?,?,?,?,?)", snapshot, owner, scope, string(raw), now.Add(10*time.Minute).Format(time.RFC3339Nano)); err != nil {
			return page, err
		}
		bytes := 0
		for i, item := range items {
			raw, err := json.Marshal(item)
			if err != nil {
				return page, err
			}
			bytes += len(raw)
			if bytes > 64<<20 {
				return page, domain.Fail("RATE_LIMITED", "Grouped library snapshot exceeds the supported size")
			}
			if _, err = tx.ExecContext(ctx, "INSERT INTO library_snapshot_items VALUES(?,?,?)", snapshot, i, raw); err != nil {
				return page, err
			}
		}
	}
	page.LibraryRevision, page.MatchingRevision = revision.Library, &revision.Matching
	rows, err := tx.QueryContext(ctx, "SELECT document FROM library_snapshot_items WHERE snapshot_id=? AND position>=? ORDER BY position LIMIT ?", snapshot, position, limit+1)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		var raw []byte
		var item library.Track
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return page, err
		}
		if err = json.Unmarshal(raw, &item); err != nil {
			rows.Close()
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		var next string
		err = tx.QueryRowContext(ctx, "SELECT id FROM library_cursors WHERE snapshot_id=? AND position=? LIMIT 1", snapshot, position+limit).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			next = domain.NewID()
			_, err = tx.ExecContext(ctx, "INSERT INTO library_cursors VALUES(?,?,?)", next, snapshot, position+limit)
		}
		if err != nil {
			return page, err
		}
		page.NextCursor = &next
	} else if cursor == "" {
		if _, err = tx.ExecContext(ctx, "DELETE FROM library_snapshots WHERE id=?", snapshot); err != nil {
			return page, err
		}
	}
	return page, tx.Commit()
}

func groupedLibraryItems(ctx context.Context, tx *sql.Tx, owner, provider string) ([]library.Track, error) {
	rows, err := tx.QueryContext(ctx, `SELECT t.document,COALESCE(l.recording_id,'') FROM tracks t
 LEFT JOIN recording_source_links l ON l.owner=t.owner AND l.source_id=t.id WHERE t.owner=? ORDER BY t.id`, owner)
	if err != nil {
		return nil, err
	}
	groups := map[string][]library.Track{}
	count, bytes := 0, 0
	for rows.Next() {
		var raw []byte
		var recording string
		if err = rows.Scan(&raw, &recording); err != nil {
			rows.Close()
			return nil, err
		}
		count++
		bytes += len(raw)
		if count > 100000 || bytes > 64<<20 {
			rows.Close()
			return nil, domain.Fail("RATE_LIMITED", "Grouped library exceeds the supported size")
		}
		var track library.Track
		if err = json.Unmarshal(raw, &track); err != nil {
			rows.Close()
			return nil, err
		}
		track.RecordingID, track.Versions = nil, nil
		key := "source:" + track.ID
		if recording != "" {
			track.RecordingID = &recording
			key = "recording:" + recording
		}
		groups[key] = append(groups[key], track)
		if len(groups[key]) > matching.MaxSources {
			rows.Close()
			return nil, domain.Fail("RATE_LIMITED", "Recording member limit exceeded")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := make([]library.Track, 0, len(groups))
	for _, members := range groups {
		visible := provider == ""
		for _, member := range members {
			visible = visible || member.ProviderID == provider
		}
		if !visible {
			continue
		}
		sort.Slice(members, func(i, j int) bool {
			if (members[i].Availability == "available") != (members[j].Availability == "available") {
				return members[i].Availability == "available"
			}
			return groupedTrackLess(members[i], members[j])
		})
		representative := members[0]
		representative.Versions = members
		items = append(items, representative)
	}
	sort.Slice(items, func(i, j int) bool { return groupedTrackLess(items[i], items[j]) })
	return items, nil
}

func groupedTrackLess(left, right library.Track) bool {
	if left.DisplayName != right.DisplayName {
		return left.DisplayName < right.DisplayName
	}
	return left.ID < right.ID
}
