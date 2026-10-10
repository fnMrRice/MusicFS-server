package library

import (
	"context"
	"io"
	"sort"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

type Runtime struct {
	ID, Name, Owner string
	Worker          *pluginhost.Worker
	StableHash      bool
}
type Probe func(context.Context, io.ReadSeeker, int64) (Observation, error)
type Service struct {
	repo     Repository
	jobs     *jobs.Service
	runtimes map[string]Runtime
	probe    Probe
}

func New(repo Repository, queue *jobs.Service, runtimes []Runtime, probe Probe) *Service {
	s := &Service{repo: repo, jobs: queue, runtimes: map[string]Runtime{}, probe: probe}
	for _, r := range runtimes {
		s.runtimes[r.ID] = r
	}
	queue.Register("scan-full", s.scan)
	queue.Register("scan-incremental", s.scan)
	return s
}
func (s *Service) Providers(ctx context.Context, user string) []Provider {
	items := []Provider{}
	for _, r := range s.runtimes {
		if r.Owner != user {
			continue
		}
		availability := "offline"
		capabilities := []string{}
		if r.Worker != nil {
			select {
			case <-r.Worker.Done():
			default:
				availability = "available"
				capabilities = []string{"library.scan", "metadata.read", "stream.resolve"}
			}
		}
		items = append(items, Provider{ID: r.ID, Name: r.Name, Capabilities: capabilities, Availability: availability, AccountState: "notRequired", ScanState: s.repo.ProviderScanState(ctx, user, r.ID)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
func (s *Service) Scan(ctx context.Context, user, provider, mode, key string) (jobs.Job, bool, error) {
	r, ok := s.runtimes[provider]
	if !ok || r.Owner != user {
		return jobs.Job{}, false, domain.Fail("NOT_FOUND", "Provider not found")
	}
	if r.Worker == nil {
		return jobs.Job{}, false, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	select {
	case <-r.Worker.Done():
		return jobs.Job{}, false, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	default:
	}
	if mode != "full" && mode != "incremental" {
		return jobs.Job{}, false, domain.Fail("INVALID_ARGUMENT", "Invalid scan mode")
	}
	return s.jobs.Submit(ctx, user, provider, "scan-"+mode, key)
}
func (s *Service) Job(ctx context.Context, user, id string) (jobs.Job, error) {
	return s.jobs.Get(ctx, user, id)
}
func (s *Service) List(ctx context.Context, user, provider, cursor string, limit int) (Page, error) {
	if provider != "" {
		r, ok := s.runtimes[provider]
		if !ok || r.Owner != user {
			return Page{}, domain.Fail("NOT_FOUND", "Provider not found")
		}
	}
	return s.repo.ListTracks(ctx, user, provider, cursor, limit)
}
func (s *Service) Track(ctx context.Context, user, id string) (Track, error) {
	return s.repo.FindTrack(ctx, user, id)
}
func (s *Service) Open(ctx context.Context, t Track, task string) (*sourceaccess.Handle, error) {
	r, ok := s.runtimes[t.ProviderID]
	if !ok || r.Owner != t.Owner || r.Worker == nil {
		return nil, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	scope := sourceaccess.Scope{UserID: t.Owner, InstanceID: t.ProviderID, TaskID: task, AssetID: t.AssetID}
	ref, err := r.Worker.Access.Register(scope, t.RootID, t.Path, t.Version, time.Hour)
	if err != nil {
		return nil, domain.Fail("RESOURCE_CHANGED", "Source is missing or has changed; rescan the library")
	}
	return r.Worker.Access.Open(ctx, scope, ref.ResourceRef)
}
func (s *Service) Revoke(provider, task string) {
	if r, ok := s.runtimes[provider]; ok && r.Worker != nil {
		r.Worker.Access.Revoke(task)
	}
}
