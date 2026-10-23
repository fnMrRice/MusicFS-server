package sqlite

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/library"
)

// RootStatistics 统计完整发布后的来源文件；扫描进度、缺失文件和媒体附件不计入。
func (s *Store) RootStatistics(ctx context.Context, owner, provider string) (library.RootStatistics, error) {
	result := library.RootStatistics{Roots: []library.RootStatistic{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT root_id,COUNT(*),SUM(size) FROM tracks
WHERE owner=? AND provider=? AND json_extract(document,'$.availability')='available'
GROUP BY root_id ORDER BY root_id`, owner, provider)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var root library.RootStatistic
		if err := rows.Scan(&root.RootID, &root.TrackCount, &root.SizeBytes); err != nil {
			return library.RootStatistics{}, err
		}
		result.Roots = append(result.Roots, root)
	}
	return result, rows.Err()
}
