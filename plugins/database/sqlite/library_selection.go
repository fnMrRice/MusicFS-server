package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (s *Store) ResolveTrackSelection(ctx context.Context, owner string, selection library.TrackSelection) (library.TrackSelectionResult, error) {
	result := library.TrackSelectionResult{Items: []library.SelectionTrack{}}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	items, err := resolveTrackSelection(ctx, tx, owner, selection)
	if err != nil {
		return result, err
	}
	// 仅为实际加入队列的曲目读取当前封面，包含默认播放文件之外的录音成员。
	for start := 0; start < len(items); start += 100 {
		end := min(start+100, len(items))
		covers := make([]*library.Track, end-start)
		for i := range covers {
			covers[i] = &items[start+i]
		}
		if err = populateTrackCovers(ctx, tx, owner, covers...); err != nil {
			return result, err
		}
	}
	// 裁剪必须早于存储 RPC 编码，完整版本与资产证据仅保留在当前事务中。
	result.Items = make([]library.SelectionTrack, 0, len(items))
	for _, track := range items {
		result.Items = append(result.Items, library.SelectionTrack{ID: track.ID, ProviderID: track.ProviderID,
			DisplayName: track.DisplayName, Title: track.Title, Artists: track.Artists, DurationMs: track.DurationMs,
			Availability: track.Availability, AssetID: track.AssetID, CoverObjectID: track.CoverObjectID, OnlineCoverPath: track.OnlineCoverPath})
	}
	result.Total = len(result.Items)
	return result, tx.Commit()
}

// resolveTrackSelection 与批量写入共用事务，避免验证修订后再读取另一份曲库。
// 分组行严格复用列表的来源过滤和默认版本规则，排除代表 ID 即排除整行。
func resolveTrackSelection(ctx context.Context, tx *sql.Tx, owner string, selection library.TrackSelection) ([]library.Track, error) {
	if owner == "" {
		return nil, domain.Fail("INVALID_ARGUMENT", "Track selection owner is required")
	}
	if err := library.ValidateTrackSelection(selection); err != nil {
		return nil, err
	}
	if selection.LibraryRevision != "" {
		var revision int64
		if err := tx.QueryRowContext(ctx, "SELECT revision FROM library_revision").Scan(&revision); err != nil {
			return nil, err
		}
		if fmt.Sprint(revision) != selection.LibraryRevision {
			return nil, domain.Fail("CONFLICT", "Library revision changed; refresh the list and select again")
		}
	}
	if selection.MatchingRevision != nil {
		revision, err := matchingRevision(ctx, tx, owner)
		if err != nil {
			return nil, err
		}
		if revision != *selection.MatchingRevision {
			return nil, domain.Fail("CONFLICT", "Matching revision changed; refresh the list and select again")
		}
	}
	load := ordinaryLibraryItems
	if selection.GroupBy == "recording" {
		load = groupedLibraryItems
	}
	items, err := load(ctx, tx, owner, selection.ProviderInstanceID)
	if err != nil {
		return nil, err
	}
	ids := selection.SourceTrackIDs
	if selection.Batch {
		ids = selection.Exclude
	}
	wanted := make(map[string]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	result := make([]library.Track, 0)
	found := 0
	for _, track := range items {
		if wanted[track.ID] {
			found++
		}
		if selection.Batch != wanted[track.ID] {
			result = append(result, track)
		}
	}
	if found != len(wanted) {
		return nil, domain.Fail("NOT_FOUND", "Selected track is not in the authorized library scope")
	}
	return result, nil
}

// ordinaryLibraryItems 与普通分页列表共用可见范围和顺序，不创建分页快照。
func ordinaryLibraryItems(ctx context.Context, tx *sql.Tx, owner, provider string) ([]library.Track, error) {
	rows, err := tx.QueryContext(ctx, ordinaryMusicSources+"SELECT t.document FROM tracks t WHERE t.owner=? AND (?='' OR t.provider=?) AND "+ordinaryMusicTrack,
		owner, owner, provider, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []library.Track{}
	bytes := 0
	for rows.Next() {
		var raw []byte
		var track library.Track
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		bytes += len(raw)
		if len(items) >= 100000 || bytes > 64<<20 {
			return nil, domain.Fail("RATE_LIMITED", "Library selection exceeds the supported size")
		}
		if err := json.Unmarshal(raw, &track); err != nil {
			return nil, err
		}
		items = append(items, track)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool { return groupedTrackLess(items[i], items[j]) })
	return items, nil
}
