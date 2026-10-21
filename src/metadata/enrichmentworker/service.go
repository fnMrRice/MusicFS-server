package enrichmentworker

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

type Runtime struct {
	ID, Name, Owner string
	Host            *scraperlocal.Host
	// Enabled distinguishes configured dependencies that failed to start from
	// intentionally disabled instances when resolving the default priority.
	Enabled bool
	Gate    *plugincontrol.Gate
	Epoch   uint64
}
type Config struct {
	MaxConcurrent int
	NCMDecoder    *ncmdecoder.Processor
	Automatic     enrichment.AutomaticPolicy
}
type runningTask struct {
	jobID  string
	cancel context.CancelFunc
}
type Service struct {
	runtimeMu sync.RWMutex
	repo      enrichment.Repository
	library   *library.Service
	content   *mediastore.ContentService
	runtimes  map[string]Runtime
	ordered   []Runtime
	config    Config
	mu        sync.Mutex
	running   map[string]runningTask
	cancel    context.CancelFunc
	started   bool
	wg        sync.WaitGroup
}

func New(repo enrichment.Repository, lib *library.Service, content *mediastore.ContentService, runtimes []Runtime, config Config) (*Service, error) {
	policy, err := config.Automatic.Normalize()
	if err != nil {
		return nil, err
	}
	config.Automatic = policy
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 1
	}
	if repo == nil || lib == nil || content == nil || config.MaxConcurrent < 1 || config.MaxConcurrent > 2 || len(runtimes) > 16 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid metadata worker configuration")
	}
	s := &Service{repo: repo, library: lib, content: content, runtimes: map[string]Runtime{}, config: config, running: map[string]runningTask{}}
	for _, r := range runtimes {
		if r.ID == "" || len(r.ID) > 128 || r.Owner == "" {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid scraper runtime")
		}
		if _, exists := s.runtimes[r.ID]; exists {
			return nil, domain.Fail("INVALID_ARGUMENT", "Duplicate scraper instance")
		}
		if r.Name == "" {
			r.Name = r.ID
		}
		s.runtimes[r.ID] = r
		s.ordered = append(s.ordered, r)
	}
	return s, nil
}

func (s *Service) Instances(_ context.Context, owner string) []enrichment.Instance {
	result := []enrichment.Instance{}
	for _, r := range s.runtimeSnapshot() {
		if r.Owner != owner {
			continue
		}
		state := "offline"
		if r.Host != nil && r.Gate.Check(context.Background()) == nil {
			state = "available"
		}
		result = append(result, enrichment.Instance{InstanceID: r.ID, Name: r.Name, PluginID: scraperlocal.PluginID, Availability: state, Fields: []string{"lyrics", "artwork"}, QueryCorrectionsSupported: false})
	}
	slices.SortFunc(result, func(a, b enrichment.Instance) int {
		if a.InstanceID < b.InstanceID {
			return -1
		}
		if a.InstanceID > b.InstanceID {
			return 1
		}
		return 0
	})
	return result
}

func (s *Service) Submit(ctx context.Context, owner string, request enrichment.JobRequest, key string) (enrichment.Job, bool, error) {
	request, err := request.Normalize()
	if err != nil {
		return enrichment.Job{}, false, err
	}
	if request.Query != nil {
		if request.Query.Title != "" || len(request.Query.Artists) > 0 {
			return enrichment.Job{}, false, domain.Fail("UNSUPPORTED", "Local scrapers do not support query corrections")
		}
		request.Query = nil
	}
	for _, id := range append(append([]string{}, request.InstanceIDs...), request.ExcludeInstanceIDs...) {
		r, ok := s.Runtime(id)
		if !ok || r.Owner != owner {
			return enrichment.Job{}, false, domain.Fail("NOT_FOUND", "Scraper instance not found")
		}
	}
	if len(request.InstanceIDs) == 0 {
		for _, r := range s.runtimeSnapshot() {
			if r.Owner == owner && r.Host != nil && r.Gate.Check(ctx) == nil && !slices.Contains(request.ExcludeInstanceIDs, r.ID) {
				request.InstanceIDs = append(request.InstanceIDs, r.ID)
			}
		}
	}
	if len(request.InstanceIDs) == 0 {
		return enrichment.Job{}, false, domain.Fail("UNSUPPORTED", "No available scraper instances")
	}
	for _, id := range request.InstanceIDs {
		r, _ := s.Runtime(id)
		if r.Host == nil {
			return enrichment.Job{}, false, domain.Fail("SOURCE_OFFLINE", "Selected scraper instance is offline")
		}
		lease, err := r.Gate.AcquireEpoch(ctx, r.Epoch)
		if err != nil {
			return enrichment.Job{}, false, err
		}
		defer lease.Close()
		if lease != nil {
			ctx = lease.Context()
		}
	}
	job, merged, err := s.repo.QueueEnrichment(ctx, owner, request, key)
	if err == nil {
		slog.InfoContext(ctx, "Metadata task queued", "jobId", job.JobID, "targets", len(request.SourceTrackIDs), "instances", len(request.InstanceIDs), "merged", merged)
	}
	return job, merged, err
}

func (s *Service) Get(ctx context.Context, owner, id string) (enrichment.Job, error) {
	return s.repo.GetEnrichmentJob(ctx, owner, id)
}
func (s *Service) Cancel(ctx context.Context, owner, id string) (enrichment.Job, error) {
	j, err := s.repo.CancelEnrichment(ctx, owner, id)
	if err == nil {
		slog.InfoContext(ctx, "Metadata task cancellation recorded", "jobId", id, "state", j.State)
		s.mu.Lock()
		for _, running := range s.running {
			if running.jobID == id {
				running.cancel()
			}
		}
		s.mu.Unlock()
	}
	return j, err
}

func (s *Service) Apply(ctx context.Context, owner, candidateID string, expectedRevision int64, key string) (mediastore.Selection, error) {
	c, err := s.repo.EnrichmentCandidate(ctx, owner, candidateID)
	if err != nil {
		return mediastore.Selection{}, err
	}
	if c.PreviewObjectID == nil {
		return mediastore.Selection{}, domain.Fail("UNSUPPORTED", "Metadata candidate has no preview content")
	}
	objectID := *c.PreviewObjectID
	return s.content.ApplyCandidate(ctx, owner, objectID, func(guard mediastore.Guard) (mediastore.Selection, error) {
		return s.repo.ApplyEnrichment(ctx, owner, candidateID, objectID, expectedRevision, key, guard)
	})
}

func (s *Service) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return domain.Fail("CONFLICT", "Metadata workers already started")
	}
	if err := s.repo.RecoverEnrichment(ctx); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.started = true
	slog.DebugContext(ctx, "Metadata workers started", "concurrency", s.config.MaxConcurrent)
	for i := 0; i < s.config.MaxConcurrent; i++ {
		s.wg.Add(1)
		go s.worker(run)
	}
	if repo, ok := s.repo.(enrichment.AutomationRepository); ok {
		s.wg.Add(1)
		go s.automationWorker(run, repo)
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

func (s *Service) worker(ctx context.Context) {
	defer s.wg.Done()
	for ctx.Err() == nil {
		work, err := s.repo.ClaimEnrichment(ctx)
		if err == nil {
			s.execute(ctx, work)
			continue
		}
		delay := 100 * time.Millisecond
		if !errors.Is(err, sql.ErrNoRows) {
			slog.DebugContext(ctx, "Metadata claim deferred", "errorCode", failureCode(err))
			delay = time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
