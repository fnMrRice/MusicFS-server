package library

import (
	"context"
	"io"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

type Runtime struct {
	ID, Name, Owner string
	SourceType      string
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
	OpenAudio          func(context.Context, string) (pluginproto.OnlineAudio, io.ReadCloser, error)
	DescribeAudio      func(context.Context, string) (pluginproto.OnlineAudio, error)
	AuthorizeAudio     func(context.Context) (*plugincontrol.Gate, error)
	AudioAccount       func(context.Context) (online.AccountView, <-chan struct{}, error)
	AudioPluginID      string
	ProviderStatus     func(context.Context) (availability, accountState string)
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
			r.SourceType = r.Worker.Manifest.SourceType
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
		accountState := "notRequired"
		capabilities := []string{}
		if r.Worker != nil {
			select {
			case <-r.Worker.Done():
			default:
				availability = "available"
				capabilities = append(capabilities, r.Worker.Manifest.Capabilities...)
			}
		}
		if r.ProviderStatus != nil {
			availability, accountState = r.ProviderStatus(ctx)
		}
		items = append(items, Provider{ID: r.ID, Name: r.Name, SourceType: r.SourceType, Plugin: r.Plugin.Clone(), Capabilities: capabilities, Availability: availability, AccountState: accountState, ScanState: s.repo.ProviderScanState(ctx, user, r.ID), Configuration: safeProviderConfiguration(r), Roots: s.providerRoots(ctx, r)})
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
func (s *Service) ScanStatus(ctx context.Context, user, provider string) (jobs.ScanStatus, error) {
	if err := s.AuthorizeProvider(ctx, user, provider); err != nil {
		return jobs.ScanStatus{}, err
	}
	return s.jobs.ScanStatus(ctx, user, provider)
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
	return s.openTrack(ctx, t, task, false)
}

// OpenPlayback 在 Windows 上使用索引中的曲目定位当前音频，版本变化由刷新索引判定。
func (s *Service) OpenPlayback(ctx context.Context, t Track, task string) (*sourceaccess.Handle, error) {
	return s.openTrack(ctx, t, task, runtime.GOOS == "windows")
}

func (s *Service) openTrack(ctx context.Context, t Track, task string, useIndexedVersion bool) (*sourceaccess.Handle, error) {
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
	if useIndexedVersion {
		handle, err := r.Worker.Access.OpenIndexed(ctx, scope, t.RootID, t.Path, t.Version)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			code := pluginproto.ErrorCode(err)
			if code == "NOT_FOUND" {
				code = "SOURCE_UNAVAILABLE"
			}
			return nil, domain.Fail(code, "Playback source could not be opened")
		}
		return handle, nil
	}
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
