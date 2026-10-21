package pluginhost

import (
	"context"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func (w *Worker) loadDisplayCount(ctx context.Context) error {
	w.displayCount = nil
	roots := make([]string, 0, len(w.config.Roots))
	for _, root := range w.config.Roots {
		roots = append(roots, root.ID)
	}
	var raw json.RawMessage
	err := w.peer.Call(ctx, "plugin.displayCount", pluginproto.DisplayCountRequest{InstanceID: w.config.InstanceID, RootIDs: roots}, &raw)
	if pluginproto.ErrorCode(err) == "UNSUPPORTED" {
		return nil
	}
	if err != nil {
		return err
	}
	count, err := plugindisplay.Decode(raw)
	if err != nil {
		return err
	}
	w.displayCount = count.Clone()
	return nil
}

// DisplayCount reads the plugin's startup projection without making RPC calls.
func (w *Worker) DisplayCount() *plugindisplay.Count {
	select {
	case <-w.done:
		return nil
	default:
		return w.displayCount.Clone()
	}
}

// DisplayCount reads the last plugin result, with the account snapshot lifetime.
func (w *OnlineWorker) DisplayCount() *plugindisplay.Count {
	select {
	case <-w.done:
		return nil
	default:
		return w.accountDisplay.count()
	}
}
