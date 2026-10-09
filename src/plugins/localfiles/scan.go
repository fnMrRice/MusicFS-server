package localfiles

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *worker) scan(ctx context.Context, r pluginproto.ScanRequest) (pluginproto.ScanPage, error) {
	if r.ScanID == "" || r.Generation < 1 || r.Limit < 1 || r.Limit > pluginproto.MaxPageItems {
		return pluginproto.ScanPage{}, fmt.Errorf("ACCESS_DENIED: invalid scan request")
	}
	var entries pluginproto.EntriesPage
	if err := w.peer.Call(ctx, "host.entries", pluginproto.EntriesRequest{RootID: r.RootID, Cursor: r.Cursor, Limit: r.Limit}, &entries); err != nil {
		return pluginproto.ScanPage{}, err
	}
	page := pluginproto.ScanPage{ScanID: r.ScanID, Generation: r.Generation, Items: make([]pluginproto.Observation, 0, len(entries.Items)), NextCursor: entries.NextCursor, Complete: entries.NextCursor == "" && len(entries.Issues) == 0, RootIdentity: entries.RootIdentity, Issues: entries.Issues}
	if _, offset, ok := strings.Cut(r.Cursor, ":"); ok {
		n, _ := strconv.Atoi(offset)
		page.Page = n / r.Limit
	}
	for _, entry := range entries.Items {
		if err := ctx.Err(); err != nil {
			return pluginproto.ScanPage{}, err
		}
		keyBytes := sha256.Sum256([]byte(r.RootID + ":" + entry.FileIdentity))
		key := hex.EncodeToString(keyBytes[:])
		var state pluginproto.StateValue
		if err := w.peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: key}, &state); err != nil {
			return pluginproto.ScanPage{}, err
		}
		var observation pluginproto.Observation
		if len(state.Value) > 0 {
			if err := json.Unmarshal(state.Value, &observation); err != nil {
				return pluginproto.ScanPage{}, err
			}
		}
		if observation.ExternalID == "" {
			var random [16]byte
			rand.Read(random[:])
			observation.ExternalID = hex.EncodeToString(random[:])
		}
		observation.RootID = entry.RootID
		observation.RelativePath = entry.RelativePath
		observation.DisplayName = filepath.Base(entry.RelativePath)
		observation.SourceRef = observation.ExternalID
		observation.ObservedVersion = entry.ObservedVersion
		observation.SizeBytes = entry.SizeBytes
		observation.MIME = entry.MIME
		observation.DurationMs = entry.DurationMs
		value, _ := json.Marshal(observation)
		if err := w.peer.Call(ctx, "host.state.compareAndSet", pluginproto.StateRequest{Key: key, Revision: state.Revision, Value: value}, nil); err != nil {
			return pluginproto.ScanPage{}, err
		}
		var lookup pluginproto.StateValue
		if err := w.peer.Call(ctx, "host.state.get", pluginproto.StateRequest{Key: "track-" + observation.ExternalID}, &lookup); err != nil {
			return pluginproto.ScanPage{}, err
		}
		if err := w.peer.Call(ctx, "host.state.compareAndSet", pluginproto.StateRequest{Key: "track-" + observation.ExternalID, Revision: lookup.Revision, Value: value}, nil); err != nil {
			return pluginproto.ScanPage{}, err
		}
		page.Items = append(page.Items, observation)
	}
	return page, nil
}
