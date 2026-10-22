package localfiles

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
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
		observation, err := w.observeEntry(ctx, entry)
		if err != nil {
			return pluginproto.ScanPage{}, err
		}
		page.Items = append(page.Items, observation)
		pluginlog.Logger(ctx).Log(ctx, pluginlog.LevelTrace, "Scan observation recorded", "page", page.Page, "itemIndex", len(page.Items), "sizeBytes", observation.SizeBytes)
	}
	pluginlog.Logger(ctx).DebugContext(ctx, "Scan page produced", "page", page.Page, "itemCount", len(page.Items), "issueCount", len(page.Issues), "complete", page.Complete)
	return page, nil
}
