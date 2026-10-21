package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func (s *Store) GetPluginSettings(ctx context.Context, owner, id string) (settings.Record, error) {
	var r settings.Record
	var raw []byte
	err := s.DB.QueryRowContext(ctx, "SELECT revision,document FROM plugin_settings WHERE owner=? AND plugin_id=?", owner, id).Scan(&r.Revision, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	err = json.Unmarshal(raw, &r.Values)
	return r, err
}
func (s *Store) PutPluginSettings(ctx context.Context, owner, id string, revision int64, values settings.Values) (settings.Record, error) {
	r := settings.Record{Revision: revision + 1, Values: values}
	if owner == "" || id == "" || revision < 0 {
		return r, domain.Fail("INVALID_ARGUMENT", "Invalid plugin settings")
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return r, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return r, err
	}
	defer tx.Rollback()
	var current int64
	err = tx.QueryRowContext(ctx, "SELECT revision FROM plugin_settings WHERE owner=? AND plugin_id=?", owner, id).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return r, err
	}
	if current != revision {
		return r, domain.Fail("CONFLICT", "Plugin settings changed")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO plugin_settings(owner,plugin_id,revision,document) VALUES(?,?,?,?) ON CONFLICT(owner,plugin_id) DO UPDATE SET revision=excluded.revision,document=excluded.document", owner, id, r.Revision, raw)
	if err != nil {
		return r, err
	}
	return r, tx.Commit()
}
