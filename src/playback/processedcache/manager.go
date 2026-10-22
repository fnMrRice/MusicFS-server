package processedcache

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Root    string
	IdleTTL time.Duration
	HardTTL time.Duration
	Now     func() time.Time
}
type entry struct {
	record      Record
	info        os.FileInfo
	refs        int
	lastRelease time.Time
	invalid     bool
}
type garbage struct {
	id, path string
	size     int64
	removed  bool
}
type Manager struct {
	mu      sync.Mutex
	repo    Repository
	budget  *Budget
	config  Config
	entries map[string]*entry
	writers map[string]*Writer
	garbage map[string]garbage
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	wg      sync.WaitGroup
	close   sync.Once
}

func New(ctx context.Context, repo Repository, budget *Budget, c Config) (*Manager, error) {
	if repo == nil || budget == nil || c.Root == "" || c.IdleTTL < 0 || c.HardTTL < 0 {
		return nil, errors.New("invalid processed cache configuration")
	}
	if c.IdleTTL == 0 {
		c.IdleTTL = 5 * time.Minute
	}
	if c.HardTTL == 0 {
		c.HardTTL = 24 * time.Hour
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if err := os.MkdirAll(c.Root, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(c.Root)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("processed cache root must be a regular directory")
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{repo: repo, budget: budget, config: c, entries: map[string]*entry{}, writers: map[string]*Writer{}, garbage: map[string]garbage{}, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	if err = m.restore(ctx); err != nil {
		cancel()
		return nil, err
	}
	go m.reap()
	return m, nil
}

func (m *Manager) now() time.Time { return m.config.Now().UTC() }
func (m *Manager) path(id, profile string) string {
	extension := ".mp3"
	if strings.HasPrefix(profile, "opus-") {
		extension = ".webm"
	}
	return filepath.Join(m.config.Root, id+extension)
}
func (m *Manager) reap() {
	defer close(m.done)
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
			ctx, cancel := context.WithTimeout(m.ctx, 5*time.Second)
			m.Collect(ctx)
			cancel()
		}
	}
}

// Close must be invoked outside playback's mutex. No cache lock is held while
// waiting for a writer that may be finishing its authorized DB publication.
func (m *Manager) Close() {
	m.close.Do(func() {
		m.cancel()
		<-m.done
		m.mu.Lock()
		pending := make([]*Writer, 0, len(m.writers))
		for _, w := range m.writers {
			pending = append(pending, w)
		}
		m.mu.Unlock()
		for _, w := range pending {
			w.Abort()
		}
		m.wg.Wait()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		m.Collect(ctx)
	})
}

// Collect only retires unreferenced objects. Failed unlinks remain accounted.
func (m *Manager) Collect(ctx context.Context) {
	m.mu.Lock()
	now := m.now()
	for key, e := range m.entries {
		if e.refs == 0 && (e.invalid || now.Sub(e.lastRelease) >= m.config.IdleTTL || now.Sub(e.record.CreatedAt) >= m.config.HardTTL) {
			delete(m.entries, key)
			path := m.path(e.record.ID, e.record.Identity.Profile)
			m.garbage[path] = garbage{id: e.record.ID, path: path, size: e.record.Size}
		}
	}
	for path, g := range m.garbage {
		// No publication or authorization callback is invoked under this lock.
		if g.removed {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			continue
		}
		m.budget.Release(g.size)
		g.size = 0
		g.removed = true
		m.garbage[path] = g
	}
	cleanup := make([]garbage, 0, len(m.garbage))
	for _, g := range m.garbage {
		if g.removed {
			cleanup = append(cleanup, g)
		}
	}
	m.mu.Unlock()
	for _, g := range cleanup {
		if g.id != "" && m.repo.ForgetProcessedCache(ctx, g.id) != nil {
			continue
		}
		m.mu.Lock()
		delete(m.garbage, g.path)
		m.mu.Unlock()
	}
}
