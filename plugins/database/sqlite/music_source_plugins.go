package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// BindMusicSourcePlugins 将配置中的实例绑定到插件；两个插件的同名来源不能互相覆盖。
func (s *Store) BindMusicSourcePlugins(ctx context.Context, providers map[string]string) error {
	if len(providers) == 0 {
		return nil
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for instance, plugin := range providers {
		if instance == "" || plugin == "" {
			return domain.Fail("INVALID_ARGUMENT", "Missing music source plugin identity")
		}
		var previous string
		err = tx.QueryRowContext(ctx, "SELECT plugin_id FROM music_source_providers WHERE provider_instance_id=?", instance).Scan(&previous)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err = tx.ExecContext(ctx, "INSERT INTO music_source_providers(provider_instance_id,plugin_id) VALUES(?,?)", instance, plugin); err != nil {
				return err
			}
		case err != nil:
			return err
		case previous != plugin:
			return domain.Fail("CONFLICT", "Music source instance already belongs to a different plugin")
		}
	}
	return tx.Commit()
}
