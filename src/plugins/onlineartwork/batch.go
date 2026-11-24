package onlineartwork

import (
	"context"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type scope struct{ owner, provider string }
type coverBatch struct {
	ids    []string
	done   chan struct{}
	values map[string]string
	err    error
}
type coverBatches struct {
	mu       sync.Mutex
	provider Metadata
	pending  map[scope]*coverBatch
}

// 旧版本没有存封面地址。同一页面同时请求多个旧曲目时，按用户及来源实例合并元数据请求。
func (b *coverBatches) lookup(ctx context.Context, key scope, id string) (string, error) {
	b.mu.Lock()
	batch := b.pending[key]
	if batch == nil {
		batch = &coverBatch{done: make(chan struct{}), values: map[string]string{}}
		b.pending[key] = batch
		time.AfterFunc(40*time.Millisecond, func() { b.load(key, batch) })
	}
	batch.ids = append(batch.ids, id)
	if len(batch.ids) == online.MaxPageItems {
		delete(b.pending, key)
	}
	b.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-batch.done:
		return batch.values[id], batch.err
	}
}

func (b *coverBatches) load(key scope, batch *coverBatch) {
	b.mu.Lock()
	if b.pending[key] == batch {
		delete(b.pending, key)
	}
	b.mu.Unlock()
	defer close(batch.done)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	data, err := b.provider.MetadataBatch(ctx, key.owner, key.provider, batch.ids)
	if err != nil {
		batch.err = err
		return
	}
	if len(data.Items) != len(batch.ids) {
		batch.err = domain.Fail("PROTOCOL_MISMATCH", "Invalid metadata batch")
		return
	}
	for i, item := range data.Items {
		if item.ExternalID != batch.ids[i] || item.Track != nil && item.Track.ExternalID != item.ExternalID {
			batch.err = domain.Fail("PROTOCOL_MISMATCH", "Invalid metadata batch identity")
			return
		}
		if item.Track != nil && item.Track.CoverURL != nil {
			batch.values[item.ExternalID] = *item.Track.CoverURL
		}
	}
}
