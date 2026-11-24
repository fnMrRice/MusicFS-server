package sqlite

import (
	"context"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

func (s *Store) OnlineTrackVersions(ctx context.Context, owner, instance string, externalIDs []string) (map[string][]onlinecache.Version, error) {
	ids, err := json.Marshal(externalIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT t.external_id, d.document FROM tracks t JOIN online_downloads d
 ON d.owner=t.owner AND d.source_id=t.id
 WHERE t.owner=? AND t.provider=? AND t.external_id IN(SELECT value FROM json_each(?))
 ORDER BY json_extract(d.document,'$.downloadedAt') DESC,d.id`, owner, instance, string(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string][]onlinecache.Version)
	err = scanDownloadVersions(rows, func(external string, version onlinecache.Version) {
		result[external] = append(result[external], version)
	})
	return result, err
}
