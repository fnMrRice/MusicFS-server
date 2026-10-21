package sqlite

import (
	"context"
	"database/sql"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

var instanceColumns = []struct{ table, column string }{
	{"tracks", "provider"},
	{"source_albums", "provider_id"},
	{"remote_playlists", "instance_id"},
	{"remote_sources", "instance_id"},
	{"jobs", "target"},
	{"provider_scan_leases", "provider"},
}

// MigratePluginInstanceIDs runs under the server data lock before any workers
// or recovery routines. Entity identities stay stable; captured execution
// tickets and audit records are never silently rebound to a new runtime.
func (s *Store) MigratePluginInstanceIDs(ctx context.Context) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	mappings, err := readInstanceMappings(ctx, tx)
	if err != nil {
		return err
	}
	if err := migrateInstanceJobs(ctx, tx); err != nil {
		return err
	}
	if len(mappings) == 0 {
		return tx.Commit()
	}
	for old, current := range mappings {
		for _, column := range instanceColumns {
			if _, err := tx.ExecContext(ctx, "UPDATE "+column.table+" SET "+column.column+"=? WHERE "+column.column+"=?", current, old); err != nil {
				return err
			}
		}
	}
	for _, document := range []struct {
		table string
		path  []string
	}{
		{"tracks", []string{"providerInstanceId"}},
		{"remote_playlists", []string{"Playlist", "instanceId"}},
		{"remote_sources", []string{"instanceId"}},
		{"remote_playlist_snapshots", []string{"entries", "*", "source", "instanceId"}},
	} {
		if err := migrateInstanceDocuments(ctx, tx, document.table, document.path); err != nil {
			return err
		}
	}
	// These are short-lived query caches. Rebuilding them prevents old filters,
	// directory hashes and provider labels from escaping through prior cursors.
	for _, table := range []string{"library_snapshots", "catalog_queries"} {
		if _, err := tx.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE library_revision SET revision=revision+1 WHERE singleton=1"); err != nil {
		return err
	}
	return tx.Commit()
}

func readInstanceMappings(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	origins := map[string]string{}
	mappings := map[string]string{}
	for _, column := range instanceColumns {
		rows, err := tx.QueryContext(ctx, "SELECT DISTINCT "+column.column+" FROM "+column.table)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var old string
			if err := rows.Scan(&old); err != nil {
				rows.Close()
				return nil, err
			}
			current := instanceid.Canonical(old)
			if other, exists := origins[current]; exists && other != old {
				rows.Close()
				return nil, domain.Fail("CONFLICT", "Stored plugin instance identities map to the same UUID; reconcile the existing instances before upgrading")
			}
			origins[current] = old
			if current != old {
				mappings[old] = current
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return mappings, nil
}
