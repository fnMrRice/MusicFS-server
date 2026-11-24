package state

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// Read 通过复合主键读取实例状态，配置预读不会建库或修改修订。
func Read(ctx context.Context, path, plugin, instance, key string) (pluginproto.StateValue, error) {
	var value pluginproto.StateValue
	store, err := scoped(plugin, instance)
	if err != nil {
		return value, err
	}
	if !keyPattern.MatchString(key) {
		return value, domain.Fail("INVALID_ARGUMENT", "Invalid database key")
	}
	db, err := openDatabase(path, "mode=ro&_pragma=query_only(1)")
	if err != nil {
		return value, err
	}
	defer db.Close()
	if err := checkSchema(ctx, db, false); err != nil {
		return value, err
	}
	store.db = db
	return store.get(ctx, key)
}
