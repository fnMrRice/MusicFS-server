package playback

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/processedcache"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

type Repository interface {
	FindAuth(context.Context, string) (auth.Session, error)
	PlaybackReset(context.Context) error
	PlaybackSave(context.Context, string, string, string, []byte, string, time.Time) error
	PlaybackOwner(context.Context, string) (string, error)
	Result(context.Context, string, string, string, string) ([]byte, error)
	SaveResult(context.Context, string, string, string, string, []byte) error
	CommitPlayback(context.Context, string, string, string, []byte, time.Time, string, string, string) error
}
type Config struct {
	NCMDecoder     *ncmdecoder.Processor
	CacheRoot      string
	MaxSessions    int
	MaxCacheBytes  int64
	RequireDSP     bool
	DisableEffects bool
}
type session struct {
	description             Description
	owner, user, path, etag string
	size                    int64
	track                   library.Track
	gain                    float64
	ctx                     context.Context
	cancel                  context.CancelFunc
	attemptCtx              context.Context
	cancelAttempt           context.CancelFunc
	active                  int
	consumers               map[string]int
	lastAccess              time.Time
	capabilities            Capabilities
	recovery                timeline.Budget
	sourceLease             *library.ReadLease
	cachePin                *processedcache.Pin
	processor               *audioffmpeg.Processor
	decodeOutput            string
	processing, decoding    *plugincontrol.Lease
	unlinkPlugins           []func() bool
}
type Service struct {
	mu        sync.Mutex
	repo      Repository
	library   *library.Service
	processor *audioffmpeg.Processor
	decoder   *ncmdecoder.Processor
	config    Config
	sessions  map[string]*session
	garbage   map[string]int64
	readers   map[string]int
	budget    *processedcache.Budget
	cache     *processedcache.Manager
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	creation  chan struct{}
	preparing map[string]context.CancelFunc
}

func New(ctx context.Context, repo Repository, lib *library.Service, processor *audioffmpeg.Processor, c Config) (*Service, error) {
	dir := filepath.Join(c.CacheRoot, "playback")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	c.CacheRoot = dir
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		if f.Type().IsRegular() && strings.HasSuffix(f.Name(), ".bin") {
			if err := os.Remove(filepath.Join(dir, f.Name())); err != nil {
				return nil, err
			}
		}
	}
	if err = repo.PlaybackReset(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{repo: repo, library: lib, processor: processor, config: c, sessions: map[string]*session{}, garbage: map[string]int64{}, ctx: ctx, cancel: cancel, done: make(chan struct{}), creation: make(chan struct{}, 1), preparing: map[string]context.CancelFunc{}}
	s.decoder = c.NCMDecoder
	s.readers = map[string]int{}
	s.budget, err = processedcache.NewBudget(c.MaxCacheBytes)
	if err != nil {
		cancel()
		return nil, err
	}
	if cacheRepo, ok := repo.(processedcache.Repository); ok {
		s.cache, err = processedcache.New(ctx, cacheRepo, s.budget, processedcache.Config{Root: filepath.Join(dir, "processed")})
		if err != nil {
			cancel()
			return nil, err
		}
	}
	go s.reap()
	slog.DebugContext(ctx, "Playback service started", "maxSessions", c.MaxSessions, "maxCacheBytes", c.MaxCacheBytes)
	return s, nil
}
func (s *Service) reap() {
	defer close(s.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-tick.C:
		}
		s.mu.Lock()
		for id, a := range s.sessions {
			if a.ctx.Err() != nil || time.Now().After(a.description.ExpiresAt) || (a.active == 0 && time.Since(a.lastAccess) > 90*time.Second) {
				s.stopLocked(context.Background(), id, a)
			}
		}
		s.collect()
		s.mu.Unlock()
	}
}
func (s *Service) collect() {
	for p, n := range s.garbage {
		if s.readers[p] != 0 {
			continue
		}
		if err := os.Remove(p); err == nil || os.IsNotExist(err) {
			delete(s.garbage, p)
			s.budget.Release(n)
		}
	}
}
func (s *Service) Close() {
	s.cancel()
	<-s.done
	s.mu.Lock()
	for id, a := range s.sessions {
		s.stopLocked(context.Background(), id, a)
	}
	s.collect()
	s.mu.Unlock()
	if s.cache != nil {
		s.cache.Close()
	}
}
func (s *Service) Revoke(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cancel := s.preparing[owner]; cancel != nil {
		cancel()
	}
	for id, a := range s.sessions {
		if a.owner == owner {
			s.stopLocked(context.Background(), id, a)
		}
	}
}
func (s *Service) Stop(ctx context.Context, who auth.Session, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.sessions[id]
	if a == nil {
		owner, err := s.repo.PlaybackOwner(ctx, id)
		if err != nil || owner != who.ID {
			return domain.Fail("NOT_FOUND", "Playback session not found")
		}
		return nil
	}
	if a.owner != who.ID {
		return domain.Fail("NOT_FOUND", "Playback session not found")
	}
	s.stopLocked(ctx, id, a)
	return nil
}
func (s *Service) stopLocked(ctx context.Context, id string, a *session) {
	a.cancel()
	a.releasePluginLeases()
	a.cachePin.Close()
	a.cachePin = nil
	if a.sourceLease != nil {
		a.sourceLease.Close()
	}
	delete(s.sessions, id)
	s.library.Revoke(a.track.ProviderID, id)
	s.garbage[a.path] = a.size
	s.collect()
	raw, _ := json.Marshal(a.description)
	if err := s.repo.PlaybackSave(ctx, id, a.owner, a.user, raw, "stopped", a.description.ExpiresAt); err != nil {
		logPlaybackFailure(ctx, "Playback stopped state persistence failed", err, "sessionId", id)
	}
	slog.InfoContext(ctx, "Playback session stopped", "sessionId", id, "instanceId", a.track.ProviderID)
}
func (s *Service) find(who auth.Session, id string) (*session, error) {
	a := s.sessions[id]
	if a == nil || a.owner != who.ID {
		return nil, domain.Fail("NOT_FOUND", "Playback session not found")
	}
	if a.ctx.Err() != nil || time.Now().After(a.description.ExpiresAt) {
		return nil, domain.Fail("SESSION_EXPIRED", "Playback session expired")
	}
	return a, nil
}
