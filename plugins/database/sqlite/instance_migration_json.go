package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
)

func migrateInstanceDocuments(ctx context.Context, tx *sql.Tx, table string, path []string) error {
	var after int64
	for {
		rows, err := tx.QueryContext(ctx, "SELECT rowid,document FROM "+table+" WHERE rowid>? ORDER BY rowid LIMIT 128", after)
		if err != nil {
			return err
		}
		type record struct {
			id  int64
			raw []byte
		}
		batch := []record{}
		for rows.Next() {
			var item record
			if err := rows.Scan(&item.id, &item.raw); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, item := range batch {
			var value any
			decoder := json.NewDecoder(bytes.NewReader(item.raw))
			decoder.UseNumber()
			if err := decoder.Decode(&value); err != nil {
				return err
			}
			if migrateInstanceJSONPath(value, path) {
				raw, err := json.Marshal(value)
				if err != nil {
					return err
				}
				if _, err := tx.ExecContext(ctx, "UPDATE "+table+" SET document=? WHERE rowid=?", raw, item.id); err != nil {
					return err
				}
			}
			after = item.id
		}
	}
}

// Paths name only persisted references. Free text, source IDs, account IDs,
// metadata and hashed request/ticket objects are outside this rewrite.
func migrateInstanceJSONPath(value any, path []string) bool {
	if len(path) == 0 {
		return false
	}
	if path[0] == "*" {
		changed := false
		if items, ok := value.([]any); ok {
			for _, item := range items {
				changed = migrateInstanceJSONPath(item, path[1:]) || changed
			}
		}
		return changed
	}
	object, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if len(path) > 1 {
		return migrateInstanceJSONPath(object[path[0]], path[1:])
	}
	old, ok := object[path[0]].(string)
	if !ok {
		return false
	}
	current := instanceid.Canonical(old)
	if old == current {
		return false
	}
	object[path[0]] = current
	return true
}
