package matching

import (
	"context"
	"errors"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/candidates"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
	"sort"
	"sync"
	"time"
)

type Automation struct {
	repo     AutomationRepository
	settings settings.Repository
	matching *Service
	detector Detector
	mu       sync.Mutex
	wake     chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
	changed  func(string)
}

func NewAutomation(repo AutomationRepository, configuration settings.Repository, service *Service, detector Detector) *Automation {
	return &Automation{repo: repo, settings: configuration, matching: service, detector: detector, wake: make(chan struct{}, 1), done: make(chan struct{})}
}
func (a *Automation) Provider() settings.Provider {
	return settings.Provider{ID: a.detector.ID, Schema: a.detector.Schema, Job: func(ctx context.Context, owner string) (settings.Job, error) {
		r, e := a.repo.MatchingRun(ctx, owner)
		return r.Job, e
	}, Action: a.Execute}
}

// OnJobChanged 在启动自动检测前设置任务状态通知。
func (a *Automation) OnJobChanged(notify func(string)) { a.changed = notify }

func (a *Automation) saveRun(ctx context.Context, r Run) error {
	err := a.repo.SaveMatchingRun(ctx, r)
	if err == nil && a.changed != nil {
		a.changed(r.Owner)
	}
	return err
}
func (a *Automation) Execute(ctx context.Context, owner, action, key string) (settings.Job, error) {
	if action != "scan" {
		return settings.Job{}, domain.Fail("NOT_FOUND", "Matching action not found")
	}
	if len(key) < 8 || len(key) > 128 {
		return settings.Job{}, domain.Fail("INVALID_ARGUMENT", "Idempotency-Key is required")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if receipt, err := a.repo.MatchingAction(ctx, owner, key); err != nil {
		return settings.Job{}, err
	} else if receipt != nil {
		return *receipt, nil
	}
	r, err := a.repo.MatchingRun(ctx, owner)
	if err != nil {
		return settings.Job{}, err
	}
	if r.Job.Status == "queued" || r.Job.Status == "running" {
		return settings.Job{}, domain.Fail("RATE_LIMITED", "Duplicate detection is already running")
	}
	id, now := domain.NewID(), time.Now().UTC()
	r = Run{Owner: owner, Key: key, Job: settings.Job{ID: &id, Status: "queued", UpdatedAt: &now}}
	if err = a.saveRun(ctx, r); err != nil {
		return settings.Job{}, err
	}
	select {
	case a.wake <- struct{}{}:
	default:
	}
	return r.Job, nil
}
func (a *Automation) Start(ctx context.Context) {
	ctx, a.cancel = context.WithCancel(ctx)
	go func() {
		defer close(a.done)
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			a.check(ctx)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			case <-a.wake:
			}
		}
	}()
}
func (a *Automation) Close() {
	if a.cancel != nil {
		a.cancel()
		<-a.done
	}
}

func (a *Automation) check(ctx context.Context) {
	owners, err := a.repo.MatchingOwners(ctx)
	if err != nil {
		return
	}
	for _, owner := range owners {
		if ctx.Err() != nil {
			return
		}
		a.checkOwner(ctx, owner)
	}
}
func (a *Automation) checkOwner(ctx context.Context, owner string) {
	a.mu.Lock()
	r, err := a.repo.MatchingRun(ctx, owner)
	configuration, e := a.settings.GetPluginSettings(ctx, owner, a.detector.ID)
	if err != nil || e != nil {
		a.mu.Unlock()
		return
	}
	values := configuration.Values
	if values == nil {
		values = settings.Defaults(a.detector.Schema)
	}
	automatic := a.detector.Automatic(values)
	requested := r.Job.Status == "queued" || r.Job.Status == "running"
	a.mu.Unlock()
	if !requested && !automatic {
		return
	}
	library, err := a.repo.MatchingLibrary(ctx, owner)
	if err != nil {
		return
	}
	if !requested && r.LibraryRevision == library.Revision && r.SettingsRevision == configuration.Revision {
		return
	}
	a.mu.Lock()
	// 读取目录期间可能收到手动请求，保留其幂等标识。
	latest, e := a.repo.MatchingRun(ctx, owner)
	if e != nil {
		a.mu.Unlock()
		return
	}
	if latest.Job.Status == "queued" || latest.Job.Status == "running" {
		r = latest
		requested = true
	}
	if r.Job.ID == nil || !requested {
		id := domain.NewID()
		r.Job.ID = &id
		r.Key = ""
	}
	now := time.Now().UTC()
	r.Job.Status = "running"
	r.Job.Total = len(library.Sources)
	r.Job.Processed = 0
	r.Job.Candidates = 0
	r.Job.ErrorCode = nil
	r.Job.UpdatedAt = &now
	r.LibraryRevision = library.Revision
	r.SettingsRevision = configuration.Revision
	err = a.saveRun(ctx, r)
	a.mu.Unlock()
	if err != nil {
		return
	}
	err = a.identify(ctx, &r, library, values)
	if ctx.Err() != nil {
		return
	}
	now = time.Now().UTC()
	r.Job.UpdatedAt = &now
	r.Job.Status = "succeeded"
	if err != nil {
		r.Job.Status = "failed"
		code := "INTERNAL"
		var failure *domain.Error
		if errors.As(err, &failure) {
			code = failure.Code
		}
		r.Job.ErrorCode = &code
		// 瞬时快照冲突会在下一次检查重试，人工决定从不回滚。
		if code == "CONFLICT" || code == "RESOURCE_CHANGED" || code == "RATE_LIMITED" {
			r.LibraryRevision = ""
		}
	}
	_ = a.saveRun(ctx, r)
}

func (a *Automation) identify(ctx context.Context, r *Run, library LibrarySnapshot, values settings.Values) error {
	scorer := a.detector.Scorer(values)
	buckets := map[string][]string{}
	keys := map[string][]string{}
	for _, source := range library.Sources {
		id := source.Input.Asset.SourceTrackID
		keys[id] = a.detector.Buckets(source)
		for _, key := range keys[id] {
			buckets[key] = append(buckets[key], id)
		}
	}
	for _, source := range library.Sources {
		if err := ctx.Err(); err != nil {
			return err
		}
		anchor := source.Input.Asset.SourceTrackID
		seen := map[string]bool{}
		pool := []string{}
		for _, key := range keys[anchor] {
			for _, id := range buckets[key] {
				if id > anchor && !seen[id] {
					seen[id] = true
					pool = append(pool, id)
				}
			}
		}
		sort.Strings(pool)
		// 每批最多20项，避免算法的结果上限截断同一歌曲的大量版本。
		for start := 0; start < len(pool); start += candidates.MaxResults {
			page, err := a.matching.refresh(ctx, r.Owner, anchor, pool[start:min(start+candidates.MaxResults, len(pool))], true, scorer)
			if err != nil {
				return err
			}
			r.Job.Candidates += len(page.Items)
		}
		r.Job.Processed++
		if r.Job.Processed%20 == 0 || r.Job.Processed == r.Job.Total {
			now := time.Now().UTC()
			r.Job.UpdatedAt = &now
			if err := a.saveRun(ctx, *r); err != nil {
				return err
			}
		}
	}
	return nil
}
