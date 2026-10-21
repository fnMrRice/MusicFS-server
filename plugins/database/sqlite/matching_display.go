package sqlite

import (
	"context"
	"database/sql"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

// 同一事务内附上完整人工录音成员，确认候选时不会把某一来源拉出原有组。
func matchingDisplay(ctx context.Context, tx *sql.Tx, owner string) (map[string]library.Track, error) {
	groups, err := groupedLibraryItems(ctx, tx, owner, "")
	if err != nil {
		return nil, err
	}
	result := map[string]library.Track{}
	for _, group := range groups {
		for _, member := range group.Versions {
			track := member
			track.Versions = group.Versions
			result[member.ID] = track
		}
	}
	return result, nil
}
