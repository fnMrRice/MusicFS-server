package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/config"
)

const configOperationColumns = "id,idempotency_key,digest,config_path,previous_revision,next_revision,plugin_id,instance_id,enabled,status"

func readConfigOperation(row rowScanner) (config.Operation, error) {
	var op config.Operation
	err := row.Scan(&op.ID, &op.Key, &op.Digest, &op.ConfigPath, &op.PreviousRevision,
		&op.NextRevision, &op.PluginID, &op.InstanceID, &op.Enabled, &op.Status)
	return op, err
}

func (s *Store) FindConfigOperation(ctx context.Context, path, key string) (config.Operation, bool, error) {
	op, err := readConfigOperation(s.DB.QueryRowContext(ctx, "SELECT "+configOperationColumns+" FROM config_operations WHERE config_path=? AND idempotency_key=?", path, key))
	if errors.Is(err, sql.ErrNoRows) {
		return op, false, nil
	}
	return op, err == nil, err
}

func (s *Store) BeginConfigOperation(ctx context.Context, op config.Operation) error {
	_, err := s.DB.ExecContext(ctx, "INSERT INTO config_operations ("+configOperationColumns+") VALUES(?,?,?,?,?,?,?,?,?,?)",
		op.ID, op.Key, op.Digest, op.ConfigPath, op.PreviousRevision, op.NextRevision, op.PluginID, op.InstanceID, op.Enabled, op.Status)
	return err
}

func (s *Store) SetConfigOperationStatus(ctx context.Context, id, status string) error {
	_, err := s.DB.ExecContext(ctx, "UPDATE config_operations SET status=? WHERE id=?", status, id)
	return err
}

func (s *Store) PendingConfigOperations(ctx context.Context, path string) ([]config.Operation, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+configOperationColumns+" FROM config_operations WHERE config_path=? AND status IN ('prepared','saved') ORDER BY rowid", path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []config.Operation
	for rows.Next() {
		op, err := readConfigOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, rows.Err()
}
