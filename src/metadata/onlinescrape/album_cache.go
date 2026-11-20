package onlinescrape

import (
	"container/list"
	"context"
	"maps"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
)

const (
	albumTTL        = 24 * time.Hour
	maxAlbumQueries = 512
	maxAlbumBytes   = 32 << 20
)

type albumScope struct {
	owner, plugin, field string
	revision             int64
}

// 只有完整查询相同才复用：关键词的摘要、地区页面和精确编号的响应可能不同。
type albumQuery struct {
	albumScope
	query scraper.Query
}

type albumEntry struct {
	key     albumQuery
	page    searchPage
	expires time.Time
	bytes   int
}

type albumLoad struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	refresh bool
	started uint64
	page    searchPage
	err     error
	// 较新的刷新完成后，旧请求仍可向调用方返回，但不能重新放入过时的发行版。
	changed albumChanges
	stale   bool
}

type albumCache struct {
	sync.Mutex
	entries map[albumQuery]*list.Element
	pending map[albumQuery]*albumLoad
	lru     list.List
	bytes   int
	serial  uint64
}

func newAlbumCache() albumCache {
	return albumCache{entries: make(map[albumQuery]*list.Element), pending: make(map[albumQuery]*albumLoad)}
}

func (s *Service) cachedAlbum(ctx context.Context, key albumQuery, refresh bool, load func(context.Context) (searchPage, error)) (searchPage, error) {
	if err := ctx.Err(); err != nil {
		return searchPage{}, err
	}
	cache := &s.albums
	cache.Lock()
	if element := cache.entries[key]; !refresh && element != nil {
		entry := element.Value.(albumEntry)
		if time.Now().Before(entry.expires) {
			cache.lru.MoveToFront(element)
			cache.Unlock()
			return entry.page, nil
		}
	}
	call := cache.pending[key]
	if call != nil {
		call.refresh = call.refresh || refresh
	} else {
		if err := s.admit(key.plugin); err != nil {
			cache.Unlock()
			return searchPage{}, err
		}
		// 第一个网页关闭不会打断其他等待者；最后一个等待者离开时取消上游。
		work, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cache.serial++
		call = &albumLoad{done: make(chan struct{}), cancel: cancel, refresh: refresh, started: cache.serial}
		cache.pending[key] = call
		go func() {
			defer cancel()
			page, err := load(work)
			<-s.gate
			cache.Lock()
			defer cache.Unlock()
			if cache.pending[key] == call {
				delete(cache.pending, key)
				if err == nil && !call.stale && !call.changed.matches(key, page) {
					cache.publish(key, call, page)
				}
			}
			call.page, call.err = page, err
			close(call.done)
		}()
	}
	call.waiters++
	cache.Unlock()
	select {
	case <-call.done:
		return call.page, call.err
	case <-ctx.Done():
		cache.Lock()
		call.waiters--
		if call.waiters == 0 && cache.pending[key] == call {
			delete(cache.pending, key)
			call.cancel()
		}
		cache.Unlock()
		return searchPage{}, ctx.Err()
	}
}

type albumReference struct{ id, url string }
type albumChanges map[albumReference]bool

func (c albumChanges) add(id, url string) {
	if id != "" {
		c[albumReference{id: id}] = true
	}
	if url != "" {
		c[albumReference{url: url}] = true
	}
}

func (c albumChanges) matches(key albumQuery, page searchPage) bool {
	if len(c) == 0 {
		return false
	}
	if c[albumReference{id: key.query.ExternalID}] || c[albumReference{url: key.query.SourceURL}] {
		return true
	}
	for _, entry := range page.items {
		if c[albumReference{id: entry.ID}] || c[albumReference{url: entry.SourceURL}] {
			return true
		}
	}
	return false
}

func (c *albumCache) publish(key albumQuery, call *albumLoad, page searchPage) {
	if call.refresh {
		changes := make(albumChanges)
		changes.add(key.query.ExternalID, key.query.SourceURL)
		for _, item := range page.items {
			changes.add(item.ID, item.SourceURL)
		}
		if prior := c.entries[key]; prior != nil {
			for _, item := range prior.Value.(albumEntry).page.items {
				changes.add(item.ID, item.SourceURL)
			}
		}
		for k, element := range c.entries {
			if k.albumScope == key.albumScope && changes.matches(k, element.Value.(albumEntry).page) {
				c.remove(element)
			}
		}
		for k, pending := range c.pending {
			if k.albumScope != key.albumScope || pending.refresh && pending.started > call.started {
				continue
			}
			// 限制单次长查询期间记录的失效编号；大量并发刷新时宁可下次重新查询。
			if len(pending.changed)+len(changes) > 256 {
				pending.stale = true
			} else if !pending.stale && len(changes) > 0 {
				if pending.changed == nil {
					pending.changed = make(albumChanges)
				}
				maps.Copy(pending.changed, changes)
			}
		}
	}
	if old := c.entries[key]; old != nil {
		c.remove(old)
	}
	if len(page.items) == 0 {
		return
	}
	q := key.query
	size := page.size + len(key.owner) + len(key.plugin) + len(key.field) + len(q.Title) + len(q.Artist) + len(q.Album) +
		len(q.ExternalID) + len(q.SourceURL) + len(q.Country) + len(q.Fingerprint)
	entry := albumEntry{key, page, time.Now().Add(albumTTL), size}
	c.entries[key] = c.lru.PushFront(entry)
	c.bytes += size
	for len(c.entries) > maxAlbumQueries || c.bytes > maxAlbumBytes {
		c.remove(c.lru.Back())
	}
}

func (c *albumCache) remove(element *list.Element) {
	entry := element.Value.(albumEntry)
	delete(c.entries, entry.key)
	c.bytes -= entry.bytes
	c.lru.Remove(element)
}
