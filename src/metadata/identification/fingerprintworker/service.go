package fingerprintworker

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type Extractor interface {
	Identity() fingerprint.Identity
	Extract(context.Context, fingerprint.Request, io.ReaderAt, int64) (fingerprint.Detail, error)
}

type Config struct {
	MaxConcurrent int
	NCMDecoder    *ncmdecoder.Processor
	Timeout       time.Duration
}

type Service struct {
	runtimeMu sync.RWMutex
	gate      *plugincontrol.Gate
	epoch     uint64
	repo      identification.FingerprintRepository
	source    identification.FingerprintSource
	extractor Extractor
	media     *mediastore.Service
	config    Config
	mu        sync.Mutex
	running   map[string]context.CancelFunc
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	started   bool
}

func New(repo identification.FingerprintRepository, source identification.FingerprintSource, extractor Extractor, media *mediastore.Service, c Config) (*Service, error) {
	if c.Timeout == 0 {
		c.Timeout = 30 * time.Second
	}
	if repo == nil || source == nil || extractor == nil || media == nil || c.MaxConcurrent < 1 || c.MaxConcurrent > 2 || c.Timeout < time.Second || c.Timeout > 2*time.Minute {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid fingerprint worker configuration")
	}
	return &Service{repo: repo, source: source, extractor: extractor, media: media, config: c, running: map[string]context.CancelFunc{}}, nil
}

func (s *Service) Submit(ctx context.Context, owner, track, key string, startMs, durationMs int64) (identification.FingerprintJob, bool, error) {
	extractor, _, gate, epoch := s.runtime()
	if extractor == nil {
		return identification.FingerprintJob{}, false, domain.Fail("PLUGIN_DISABLED", "Fingerprint audio dependency is unavailable")
	}
	lease, err := gate.AcquireEpoch(ctx, epoch)
	if err != nil {
		return identification.FingerprintJob{}, false, err
	}
	defer lease.Close()
	if lease != nil {
		ctx = lease.Context()
	}
	if _, _, err := (fingerprint.Request{TaskID: "request", AssetID: "request", StartMs: startMs, DurationMs: durationMs}).Samples(); err != nil {
		return identification.FingerprintJob{}, false, domain.Fail("INVALID_ARGUMENT", "Invalid fingerprint range")
	}
	target, err := s.source.ResolveHashTarget(ctx, owner, track)
	if err != nil {
		return identification.FingerprintJob{}, false, err
	}
	request := identification.FingerprintRequest{StartMs: startMs, DurationMs: durationMs, ExtractorIdentity: extractor.Identity()}
	j, merged, err := s.repo.QueueFingerprint(ctx, owner, identification.FingerprintTarget{HashTarget: target, FingerprintRequest: request}, key, false)
	if err == nil {
		slog.InfoContext(ctx, "Fingerprint task queued", "jobId", j.ID, "merged", merged)
	}
	return j, merged, err
}

func (s *Service) Get(ctx context.Context, owner, id string) (identification.FingerprintJob, error) {
	return s.repo.GetFingerprintJob(ctx, owner, id)
}
func (s *Service) Cancel(ctx context.Context, owner, id string) (identification.FingerprintJob, error) {
	j, err := s.repo.CancelFingerprint(ctx, owner, id)
	if err == nil {
		slog.InfoContext(ctx, "Fingerprint task cancellation recorded", "jobId", id, "state", j.Status)
		s.mu.Lock()
		cancel := s.running[id]
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return j, err
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return domain.Fail("CONFLICT", "Fingerprint workers already started")
	}
	if err := s.media.Recover(ctx); err != nil {
		return err
	}
	if err := s.repo.RecoverFingerprints(ctx); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.started = true
	slog.DebugContext(ctx, "Fingerprint workers started", "concurrency", s.config.MaxConcurrent)
	for i := 0; i < s.config.MaxConcurrent; i++ {
		s.wg.Add(1)
		go s.worker(run)
	}
	return nil
}

func (s *Service) Close() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}

func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for ctx.Err() == nil {
		j, err := s.repo.ClaimFingerprint(ctx)
		if err != nil {
			delay := 100 * time.Millisecond
			if !errors.Is(err, sql.ErrNoRows) {
				slog.DebugContext(ctx, "Fingerprint claim deferred", "errorCode", failureCode(err))
				delay = time.Second
			}
			if !pause(ctx, delay) {
				return
			}
			continue
		}
		s.execute(ctx, j)
	}
}
