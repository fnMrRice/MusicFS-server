package library

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"io"
	"time"
)

type Runtime struct {
	ID, Name, Owner string
	Worker          *pluginhost.Worker
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
func (s *Service) scan(ctx context.Context, j jobs.Job) (int, error) {
	r, ok := s.runtimes[j.ProviderInstanceID]
	if !ok || r.Owner != j.Owner || r.Worker == nil {
		return 0, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	defer r.Worker.Access.Revoke(j.ID)
	all := []Observation{}
	cursor := ""
	for {
		page, err := r.Worker.Scan(ctx, pluginproto.ScanRequest{RootID: r.ID, ScanID: j.ID, Generation: j.Generation, Cursor: cursor, Limit: 200})
		if err != nil {
			return len(all), err
		}
		if page.ScanID != j.ID || page.Generation != j.Generation || len(page.Issues) > 0 {
			return len(all), domain.Fail("TEMPORARY_FAILURE", "Scan is incomplete")
		}
		for _, o := range page.Items {
			item := Observation{Observation: o, Artists: []string{}}
			if s.probe != nil {
				scope := sourceaccess.Scope{UserID: j.Owner, InstanceID: r.ID, TaskID: j.ID, AssetID: o.ObservedVersion}
				ref, e := r.Worker.Access.Register(scope, o.RootID, o.RelativePath, o.ObservedVersion, time.Minute)
				if e == nil {
					h, e := r.Worker.Access.Open(ctx, scope, ref.ResourceRef)
					if e == nil {
						meta, e := s.probe(ctx, h, o.SizeBytes)
						valid := h.Validate()
						h.Close()
						if valid != nil {
							return len(all), valid
						}
						if e == nil {
							item.Codec = meta.Codec
							item.Container = meta.Container
							item.DurationMs = meta.DurationMs
							item.BitrateBps = meta.BitrateBps
							item.Title = meta.Title
							item.Artists = meta.Artists
							item.MIME = meta.MIME
						}
					}
				}
			}
			all = append(all, item)
			if len(all) > 50000 {
				return len(all), domain.Fail("RATE_LIMITED", "Library entry limit exceeded")
			}
		}
		if page.NextCursor == "" {
			if !page.Complete {
				return len(all), domain.Fail("TEMPORARY_FAILURE", "Scan did not complete")
			}
			break
		}
		if page.NextCursor == cursor {
			return len(all), domain.Fail("CONFLICT", "Scan cursor did not advance")
		}
		cursor = page.NextCursor
	}
	return len(all), s.repo.Publish(ctx, j, all)
}
