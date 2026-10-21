package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func (s *Store) SavePluginOperation(ctx context.Context, op plugincontrol.Operation) error {
	raw, err := json.Marshal(op)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, "INSERT INTO plugin_operations(id,owner,document) VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET document=excluded.document WHERE plugin_operations.owner=excluded.owner", op.ID, op.Owner, raw)
	return err
}
func (s *Store) GetPluginOperation(ctx context.Context, owner, id string) (plugincontrol.Operation, error) {
	var raw []byte
	var op plugincontrol.Operation
	err := s.DB.QueryRowContext(ctx, "SELECT document FROM plugin_operations WHERE id=? AND owner=?", id, owner).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return op, domain.Fail("NOT_FOUND", "Plugin operation not found")
	}
	if err != nil {
		return op, err
	}
	err = json.Unmarshal(raw, &op)
	op.Owner = owner
	return op, err
}
func (s *Store) RecoverPluginOperations(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT owner,document FROM plugin_operations")
	if err != nil {
		return err
	}
	items := []plugincontrol.Operation{}
	for rows.Next() {
		var raw []byte
		var owner string
		if err = rows.Scan(&owner, &raw); err != nil {
			break
		}
		var op plugincontrol.Operation
		if err = json.Unmarshal(raw, &op); err != nil {
			break
		}
		op.Owner = owner
		if op.Status == "applying" || op.Status == "draining" || op.Status == "saved" {
			op.Status = "superseded"
			code := "SERVER_RESTARTED"
			op.ErrorCode = &code
			op.UpdatedAt = time.Now().UTC()
			items = append(items, op)
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	for _, op := range items {
		if err = s.SavePluginOperation(ctx, op); err != nil {
			return err
		}
	}
	return nil
}
