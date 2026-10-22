package library

import (
	"context"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

type Runtime struct {
	ID, Name, Owner string
	// Plugin is a bundled plugin declaration supplied by bootstrap. A verified
	// worker takes precedence; neither configuration nor instance aliases set it.
	Plugin             *pluginproto.PluginIdentity
	Worker             *pluginhost.Worker
	StableHash         bool
	ConfigRevision     string
	ImportDefaults     pluginproto.ImportOptions
	RootImportDefaults map[string]pluginproto.ImportOptions
	RootNames          map[string]string
	WriteBack          map[string]RootWriteBack
	Configuration      *pluginproto.Configuration
	Gate               *plugincontrol.Gate
}
type Probe func(context.Context, io.ReadSeeker, int64) (Observation, error)
type ProbeSession func(context.Context) (context.Context, Probe, func(), error)
type Service struct {
	mu                  sync.RWMutex
	repo                Repository
	jobs                *jobs.Service
	runtimes            map[string]Runtime
	probe               Probe
	probeSession        ProbeSession
	writeBackProbeReady func() bool
	gates               map[string]*providerGate
}

func New(repo Repository, queue *jobs.Service, runtimes []Runtime, probe Probe) *Service {
	s := &Service{repo: repo, jobs: queue, runtimes: map[string]Runtime{}, probe: probe, gates: map[string]*providerGate{}}
	for _, r := range runtimes {
		r.Plugin = r.Plugin.Clone()
		if r.Worker != nil {
			r.Plugin = r.Worker.PluginIdentity()
		}
		s.runtimes[r.ID] = r
		s.gates[r.ID] = &providerGate{barriers: map[string]bool{}, tasks: map[string]*ReadLease{}, changed: make(chan struct{})}
	}
	queue.Register("scan-full", s.scan)
	queue.Register("scan-incremental", s.scan)
	return s
}
func (s *Service) Providers(ctx context.Context, user string) []Provider {
	items := []Provider{}
	for _, r := range s.Runtimes() {
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
				capabilities = append(capabilities, r.Worker.Manifest.Capabilities...)
			}
		}
		items = append(items, Provider{ID: r.ID, Name: r.Name, Plugin: r.Plugin.Clone(), Capabilities: capabilities, Availability: availability, AccountState: "notRequired", ScanState: s.repo.ProviderScanState(ctx, user, r.ID), Configuration: safeProviderConfiguration(r), Roots: s.providerRoots(ctx, r)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}
func (s *Service) Scan(ctx context.Context, user, provider string, input ScanInput, key string) (jobs.Job, bool, error) {
	lease, err := s.HoldRead(ctx, user, provider)
	if err != nil {
		return jobs.Job{}, false, err
	}
	defer lease.Close()
	ctx = lease.Context()
	plan, err := s.BuildScanPlan(ctx, user, provider, input)
	if err != nil {
		return jobs.Job{}, false, err
	}
	return s.jobs.Submit(ctx, user, provider, plan, key)
}
func (s *Service) Job(ctx context.Context, user, id string) (jobs.Job, error) {
	return s.jobs.Get(ctx, user, id)
}
func (s *Service) List(ctx context.Context, user, provider, cursor string, limit int) (Page, error) {
	if provider != "" {
		r, ok := s.Runtime(provider)
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
	lease, err := s.holdTask(ctx, t.Owner, t.ProviderID, task)
	if err != nil {
		return nil, err
	}
	r := lease.runtime
	if r.Worker == nil {
		return nil, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	ctx = lease.Context()
	scope := sourceaccess.Scope{UserID: t.Owner, InstanceID: t.ProviderID, TaskID: task, AssetID: t.AssetID}
	ref, err := r.Worker.Access.Register(scope, t.RootID, t.Path, t.Version, time.Hour)
	if err != nil {
		return nil, domain.Fail("RESOURCE_CHANGED", "Source is missing or has changed; rescan the library")
	}
	return r.Worker.Access.Open(ctx, scope, ref.ResourceRef)
}
func (s *Service) Revoke(provider, task string) {
	if r, ok := s.Runtime(provider); ok && r.Worker != nil {
		r.Worker.Access.Revoke(task)
	}
	s.revokeTask(provider, task)
}
