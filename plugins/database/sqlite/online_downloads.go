package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

func (s *Store) ListOnlineDownloads(ctx context.Context) ([]onlinecache.Record, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT lookup_key,owner,plugin_id,source_id,document FROM online_downloads ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []onlinecache.Record
	for rows.Next() {
		var r onlinecache.Record
		var raw string
		if err = rows.Scan(&r.Key, &r.Owner, &r.Plugin, &r.Source, &raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(raw), &r.Version); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

func (s *Store) SaveOnlineDownload(ctx context.Context, r onlinecache.Record) error {
	raw, err := json.Marshal(r.Version)
	if err != nil {
		return err
	}
	result, err := s.DB.ExecContext(ctx, `INSERT INTO online_downloads(id,lookup_key,owner,plugin_id,source_id,document)
 SELECT ?,?,?,?,?,? WHERE EXISTS(SELECT 1 FROM tracks WHERE owner=? AND id=?)`, r.ID, r.Key, r.Owner, r.Plugin, r.Source, string(raw), r.Owner, r.Source)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return domain.Fail("RESOURCE_CHANGED", "Online source no longer exists")
	}
	return err
}

func (s *Store) ForgetOnlineDownload(ctx context.Context, target onlinecache.DownloadTarget) error {
	_, err := s.DB.ExecContext(ctx, "DELETE FROM online_downloads WHERE owner=? AND source_id=? AND id=?", target.Owner, target.SourceID, target.VersionID)
	return err
}

func readDownloadVersions(ctx context.Context, q trackCoverQuery, owner, ids string, apply func(string, onlinecache.Version)) error {
	rows, err := q.QueryContext(ctx, `SELECT source_id,document FROM online_downloads WHERE owner=? AND source_id IN(SELECT value FROM json_each(?))
 ORDER BY json_extract(document,'$.downloadedAt') DESC,id`, owner, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	return scanDownloadVersions(rows, apply)
}

func scanDownloadVersions(rows *sql.Rows, apply func(string, onlinecache.Version)) error {
	for rows.Next() {
		var id, raw string
		var v onlinecache.Version
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return err
		}
		apply(id, v)
	}
	return rows.Err()
}
