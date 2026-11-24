package onlineartwork

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type scope struct{ owner, provider string }

type coverQuery struct {
	ctx   context.Context
	id    string
	done  chan struct{}
	value string
	err   error
}

type coverBatches struct {
	mu       sync.Mutex
	provider Metadata
	pending  map[scope][]*coverQuery
	slots    chan struct{}
}

// 同一账号和实例只运行一个后台元数据请求。当前批次等待平台期间，新曲目继续积攒到下一批。
func (b *coverBatches) lookup(ctx context.Context, key scope, id string) (string, error) {
	q := &coverQuery{ctx: ctx, id: id, done: make(chan struct{})}
	b.mu.Lock()
	_, running := b.pending[key]
	b.pending[key] = append(b.pending[key], q)
	if !running {
		time.AfterFunc(40*time.Millisecond, func() { b.drain(key) })
	}
	b.mu.Unlock()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-q.done:
		return q.value, q.err
	}
}

func (b *coverBatches) drain(key scope) {
	for {
		b.mu.Lock()
		waiting := b.pending[key]
		batch := make([]*coverQuery, 0, min(len(waiting), online.MaxPageItems))
		for len(waiting) > 0 && len(batch) < online.MaxPageItems {
			q := waiting[0]
			waiting = waiting[1:]
			if q.ctx.Err() == nil {
				batch = append(batch, q)
			}
		}
		if len(batch) == 0 {
			delete(b.pending, key)
			b.mu.Unlock()
			return
		}
		b.pending[key] = waiting
		b.mu.Unlock()

		err := b.load(key, batch)
		for _, q := range batch {
			q.err = err
			close(q.done)
		}
	}
}

func (b *coverBatches) load(key scope, batch []*coverQuery) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var listeners atomic.Int32
	listeners.Store(int32(len(batch)))
	ids := make([]string, len(batch))
	for i, q := range batch {
		ids[i] = q.id
		stop := context.AfterFunc(q.ctx, func() {
			if listeners.Add(-1) == 0 {
				cancel()
			}
		})
		defer stop()
	}
	data, err := playlists.RetryRemoteRead(ctx, func(ctx context.Context) (online.TrackBatch, error) {
		// 全部封面实例最多占宿主四个普通 RPC 槽中的两个；退避期间释放槽位。
		select {
		case b.slots <- struct{}{}:
			defer func() { <-b.slots }()
		case <-ctx.Done():
			return online.TrackBatch{}, ctx.Err()
		}
		return b.provider.MetadataBatch(ctx, key.owner, key.provider, ids)
	})
	if err != nil {
		return err
	}
	if len(data.Items) != len(batch) {
		return domain.Fail("PROTOCOL_MISMATCH", "Invalid metadata batch")
	}
	for i, item := range data.Items {
		if item.ExternalID != ids[i] || item.Track != nil && item.Track.ExternalID != ids[i] {
			return domain.Fail("PROTOCOL_MISMATCH", "Invalid metadata batch identity")
		}
		if item.Track != nil && item.Track.CoverURL != nil {
			batch[i].value = *item.Track.CoverURL
		}
	}
	return nil
}
