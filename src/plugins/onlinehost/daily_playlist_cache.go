package onlinehost

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// 每个目录、歌单或曲目批次只保存最后一次成功读取；账号绑定参加校验。
type dailyPlaylistCache struct {
	dir    string
	mu     sync.Mutex
	active map[string]chan struct{}
}

func WithPlaylistCache(dir string) func(*Service) {
	return func(s *Service) {
		s.playlistCache = &dailyPlaylistCache{dir: dir, active: make(map[string]chan struct{})}
	}
}

func (c *dailyPlaylistCache) lock(ctx context.Context, key string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c.mu.Lock()
		wait := c.active[key]
		if wait == nil {
			wait = make(chan struct{})
			c.active[key] = wait
			c.mu.Unlock()
			return func() {
				c.mu.Lock()
				delete(c.active, key)
				close(wait)
				c.mu.Unlock()
			}, nil
		}
		c.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type playlistScope struct {
	account, binding, config, revision string
}

func (s *Service) playlistScope(ctx context.Context, owner, id string) (playlistScope, error) {
	r, err := s.Runtime(ctx, owner, id, "")
	if err != nil {
		return playlistScope{}, err
	}
	p := scopedRecommendation{host: s, owner: owner, id: id}
	account, revision, binding, err := p.RecommendationAccountScope(ctx)
	return playlistScope{account, binding, r.ConfigRevision, revision}, err
}

func (s *Service) checkPlaylistScope(ctx context.Context, owner, id string, expected playlistScope) error {
	current, err := s.playlistScope(ctx, owner, id)
	if err == nil && current != expected {
		return domain.Fail("RESOURCE_CHANGED", "Playlist account or instance changed")
	}
	return err
}

type savedPlaylistValue[T any] struct {
	Day   string `json:"day"`
	Scope string `json:"scope"`
	Value T      `json:"value"`
}

func dailyPlaylistValue[T any](ctx context.Context, s *Service, owner, id, key string, refresh bool, fetch func(context.Context) (T, error)) (T, error) {
	var empty T
	scope, err := s.playlistScope(ctx, owner, id)
	if err != nil {
		return empty, err
	}
	return cachedPlaylistValue(ctx, s, owner, id, scope, key, "", refresh, fetch)
}

func cachedPlaylistValue[T any](ctx context.Context, s *Service, owner, id string, scope playlistScope, key, version string, refresh bool, fetch func(context.Context) (T, error)) (T, error) {
	var empty T
	c := s.playlistCache
	name := online.Digest([]string{owner, id, key}) + ".json"
	unlock, err := c.lock(ctx, name)
	if err != nil {
		return empty, err
	}
	defer unlock()
	path := filepath.Join(c.dir, name)
	day := time.Now().Format(time.DateOnly)
	binding := online.Digest([]string{scope.account, scope.binding, scope.config, version})
	if !refresh {
		raw, err := os.ReadFile(path)
		var entry savedPlaylistValue[T]
		if err == nil && json.Unmarshal(raw, &entry) == nil && entry.Day == day && entry.Scope == binding {
			if err := s.checkPlaylistScope(ctx, owner, id, scope); err != nil {
				return empty, err
			}
			return entry.Value, nil
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return empty, err
		}
	}
	value, err := fetch(ctx)
	if err != nil {
		return empty, err
	}
	if err := s.checkPlaylistScope(ctx, owner, id, scope); err != nil {
		return empty, err
	}
	if err := os.MkdirAll(c.dir, 0700); err != nil {
		return empty, err
	}
	file, err := os.CreateTemp(c.dir, ".playlist-*")
	if err != nil {
		return empty, err
	}
	defer os.Remove(file.Name())
	err = json.NewEncoder(file).Encode(savedPlaylistValue[T]{day, binding, value})
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	return value, err
}
