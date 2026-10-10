package localfiles

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
)

func (w *worker) scan(ctx context.Context, r pluginproto.ScanRequest) (pluginproto.ScanPage, error) {
	if r.ScanID == "" || r.Generation < 1 || r.GrantID == "" || r.PlanDigest == "" || r.Limit < 1 || r.Limit > pluginproto.MaxPageItems {
		return pluginproto.ScanPage{}, fmt.Errorf("ACCESS_DENIED: invalid scan request")
	}
	var entries pluginproto.EntriesPage
	if err := w.peer.Call(ctx, "host.entries", pluginproto.EntriesRequest{GrantID: r.GrantID, RootID: r.RootID, Cursor: r.Cursor, Limit: r.Limit}, &entries); err != nil {
		return pluginproto.ScanPage{}, err
	}
	page := pluginproto.ScanPage{PlanDigest: r.PlanDigest, ScanID: r.ScanID, Generation: r.Generation, Items: make([]pluginproto.Observation, 0, len(entries.Items)), NextCursor: entries.NextCursor, Complete: entries.NextCursor == "" && len(entries.Issues) == 0, RootIdentity: entries.RootIdentity, Issues: entries.Issues}
	if _, offset, ok := strings.Cut(r.Cursor, ":"); ok {
		n, _ := strconv.Atoi(offset)
		page.Page = n / r.Limit
	}
	for _, entry := range entries.Items {
		if err := ctx.Err(); err != nil {
			return pluginproto.ScanPage{}, err
		}
		externalID, err := w.identity(ctx, entry)
		if err != nil {
			return pluginproto.ScanPage{}, err
		}
		observation := pluginproto.Observation{ExternalID: externalID}
		observation.RootID = entry.RootID
		observation.RelativePath = entry.RelativePath
		observation.DisplayName = filepath.Base(entry.RelativePath)
		observation.SourceRef = observation.ExternalID
		observation.ObservedVersion = entry.ObservedVersion
		observation.SizeBytes = entry.SizeBytes
		observation.MIME = entry.MIME
		observation.DurationMs = entry.DurationMs
		value, _ := json.Marshal(observation)
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
