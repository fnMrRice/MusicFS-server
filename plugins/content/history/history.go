// Package history 提供内置内容插件可复用的作品和单集收听记录。
package history

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type History struct {
	state contenttype.HistoryState
	key   func(contenttype.HistoryEntry) string
}

func ByWork(state contenttype.HistoryState) *History {
	return &History{state, func(e contenttype.HistoryEntry) string { return e.WorkID }}
}

func ByEpisode(state contenttype.HistoryState) *History {
	return &History{state, func(e contenttype.HistoryEntry) string { return e.WorkID + "/" + e.SourceTrackID }}
}

func accountKey(owner string) string { return fmt.Sprintf("history-%x", sha256.Sum256([]byte(owner))) }

func (h *History) read(ctx context.Context, owner string) ([]contenttype.HistoryEntry, int64, error) {
	value, err := h.state.Get(ctx, accountKey(owner))
	items := []contenttype.HistoryEntry{}
	if err == nil && value.Revision != 0 {
		err = json.Unmarshal(value.Value, &items)
	}
	return items, value.Revision, err
}

func (h *History) List(ctx context.Context, owner string) ([]contenttype.HistoryEntry, error) {
	items, _, err := h.read(ctx, owner)
	return items, err
}

func (h *History) Record(ctx context.Context, owner string, entry contenttype.HistoryEntry) error {
	for {
		items, revision, err := h.read(ctx, owner)
		if err != nil {
			return err
		}
		index := slices.IndexFunc(items, func(e contenttype.HistoryEntry) bool { return h.key(e) == h.key(entry) })
		if index >= 0 {
			if !items[index].RecordedAt.Before(entry.RecordedAt) {
				return nil
			}
			items = slices.Delete(items, index, index+1)
		}
		items = append(items, entry)
		slices.SortStableFunc(items, func(a, b contenttype.HistoryEntry) int { return b.RecordedAt.Compare(a.RecordedAt) })
		// 每个用户最多保留最近的 100 项，按私有状态容量截断最早的记录。
		items = items[:min(len(items), 100)]
		raw, err := json.Marshal(items)
		for err == nil && len(raw) > 512<<10 && len(items) > 1 {
			items = items[:len(items)-1]
			raw, err = json.Marshal(items)
		}
		if err != nil {
			return err
		}
		_, err = h.state.CompareAndSet(ctx, pluginproto.StateRequest{Key: accountKey(owner), Revision: revision, Value: raw})
		var conflict *fault.Error
		if !errors.As(err, &conflict) || conflict.Code != "PRECONDITION_FAILED" {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Millisecond):
		}
	}
}
