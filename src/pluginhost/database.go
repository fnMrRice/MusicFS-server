package pluginhost

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugindb"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *Worker) database(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	var r pluginproto.StateRequest
	if json.Unmarshal(raw, &r) != nil {
		return nil, pluginproto.Failure("INVALID_ARGUMENT", "Invalid database request")
	}
	// Managed settings are readable here but only mutable through the host's
	// validated lifecycle API. Ordinary plugin keys support independent writes.
	if method != "host.database.get" && strings.HasPrefix(r.Key, "host-") {
		return nil, pluginproto.Failure("ACCESS_DENIED", "Protected database key")
	}
	path := w.config.DatabasePath
	if path == "" {
		path = filepath.Join(w.config.StateDir, "plugin-config.sqlite")
	}
	db, err := plugindb.Open(ctx, path, "musicfs.localfiles", w.config.InstanceID)
	if err != nil {
		return nil, pluginproto.Failure("TEMPORARY_FAILURE", "Plugin database is unavailable")
	}
	defer db.Close()
	var value pluginproto.StateValue
	if method == "host.database.get" {
		value, err = db.Get(ctx, r.Key)
	} else {
		value, err = db.CompareAndSet(ctx, r)
	}
	if err != nil {
		var detail *domain.Error
		if errors.As(err, &detail) {
			code := detail.Code
			if code == "PRECONDITION_FAILED" {
				code = "RESOURCE_CHANGED"
			}
			return nil, pluginproto.Failure(code, "Plugin database request failed")
		}
		return nil, pluginproto.Failure("TEMPORARY_FAILURE", "Plugin database request failed")
	}
	return value, nil
}
