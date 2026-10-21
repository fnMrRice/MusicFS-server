package writebackhost

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/writeback"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
)

type Config struct {
	Policy         writeback.Policy
	AllowOverwrite bool
	Probe          library.Probe
	ProbeSession   library.ProbeSession
}

type Service struct {
	repo       Repository
	lib        *library.Service
	content    *mediastore.ContentService
	journal    writeback.Journal
	editor     *writeback.Service
	cfg        Config
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	mu         sync.Mutex
	recoveryMu sync.Mutex
	retries    map[string]string
	active     atomic.Pointer[string]
}

var errBusy = errors.New("writeback source is in use")

func New(repo Repository, lib *library.Service, media *mediastore.ContentService, journal writeback.Journal, _ []library.Runtime, cfg Config) (*Service, error) {
	s := &Service{repo: repo, lib: lib, content: media, journal: journal, cfg: cfg, retries: map[string]string{}}
	var err error
	s.editor, err = writeback.New(cfg.Policy, journal, coordinator{s})
	return s, err
}

func normalize(r Request) (Request, error) {
	if r.SourceTrackID == "" || r.AssetID == "" || r.AssetVersion == "" || r.ExpectedRevision < 0 || len(r.Fields) < 1 || len(r.Fields) > 2 {
		return r, domain.Fail("INVALID_ARGUMENT", "Invalid writeback request")
	}
	for _, id := range []string{r.SourceTrackID, r.AssetID, r.AssetVersion} {
		if len(id) > 256 || strings.ContainsAny(id, "\x00\r\n") {
			return r, domain.Fail("INVALID_ARGUMENT", "Invalid writeback target")
		}
	}
	r.Fields = append([]Field(nil), r.Fields...)
	seen := map[string]bool{}
	for i, f := range r.Fields {
		if (f.Name != "lyrics" && f.Name != "artwork") || seen[f.Name] || (f.Name == "artwork" && f.Language != "") {
			return r, domain.Fail("INVALID_ARGUMENT", "Choose at most one selection per writeback field")
		}
		seen[f.Name] = true
		f.Language = mediastore.CanonicalLanguage(f.Name, f.Language)
		m := mediastore.ContentMetadata{Target: mediastore.ContentTarget{SourceTrackID: r.SourceTrackID, AssetID: r.AssetID, AssetVersion: r.AssetVersion}, Field: f.Name, Language: f.Language, Format: "text", Origin: mediastore.Origin{Kind: "edit"}}
		if f.Name == "artwork" {
			m.Format = "png"
		}
		if err := m.Validate(); err != nil {
			return r, err
		}
		r.Fields[i] = f
	}
	sort.Slice(r.Fields, func(i, j int) bool { return r.Fields[i].Name < r.Fields[j].Name })
	return r, nil
}

func (s *Service) Submit(ctx context.Context, owner string, request Request, key string) (Job, bool, error) {
	r, err := normalize(request)
	if err != nil {
		return Job{}, false, err
	}
	if len(key) < 8 || len(key) > 128 {
		return Job{}, false, domain.Fail("INVALID_ARGUMENT", "Invalid writeback operation key")
	}
	if j, ok, e := s.repo.ReplayWriteback(ctx, owner, r, key); e != nil || ok {
		if e == nil && ok {
			j, e = s.Get(ctx, owner, j.ID)
		}
		return j, ok, e
	}
	if !s.cfg.Policy.Enabled {
		return Job{}, false, domain.Fail("ACCESS_DENIED", "Source writeback is disabled")
	}
	if s.cfg.Probe == nil {
		return Job{}, false, domain.Fail("UNSUPPORTED", "Source writeback requires a metadata probe")
	}
	if r.Overwrite && !s.cfg.AllowOverwrite {
		return Job{}, false, domain.Fail("ACCESS_DENIED", "Overwriting source tags is disabled")
	}
	t, err := s.lib.Track(ctx, owner, r.SourceTrackID)
	if err != nil {
		return Job{}, false, err
	}
	if t.AssetID != r.AssetID || t.Version != r.AssetVersion || t.Availability != "available" {
		return Job{}, false, domain.Fail("RESOURCE_CHANGED", "Writeback source changed")
	}
	root, err := s.grant(owner, t.ProviderID, t.RootID, r.Fields)
	if err != nil {
		return Job{}, false, err
	}
	if _, err = writeback.InspectCapability(root); err != nil {
		return Job{}, false, err
	}
	read, err := s.lib.HoldRead(ctx, owner, t.ProviderID)
	if err != nil {
		return Job{}, false, err
	}
	defer read.Close()
	ctx = read.Context()
	if s.cfg.ProbeSession != nil {
		var release func()
		ctx, _, release, err = s.cfg.ProbeSession(ctx)
		if err != nil {
			return Job{}, false, err
		}
		defer release()
	}
	runtime, _ := s.lib.Runtime(t.ProviderID)
	w := runtime.Worker
	if w == nil {
		return Job{}, false, domain.Fail("SOURCE_OFFLINE", "Writeback provider is offline")
	}
	if _, err = s.boundRoot(owner, t.ProviderID, t.RootID, w.Access); err != nil {
		return Job{}, false, err
	}
	entry, err := w.Access.Inspect(t.RootID, t.Path)
	if err != nil {
		return Job{}, false, domain.Fail("RESOURCE_CHANGED", "Writeback source is unavailable")
	}
	if entry.ObservedVersion != t.Version || entry.SizeBytes != t.Size || entry.LinkCount != 1 || !strings.HasSuffix(strings.ToLower(t.Path), ".flac") {
		return Job{}, false, domain.Fail("UNSUPPORTED", "Source does not support safe FLAC writeback")
	}
	target := writeback.Target{SourceTrackID: t.ID, AssetID: t.AssetID, AssetVersion: t.Version, InstanceID: t.ProviderID, RootID: t.RootID, RelativePath: t.Path, FileIdentity: entry.FileIdentity}
	return s.repo.QueueWriteback(ctx, owner, r, target, key)
}

func (s *Service) grant(owner, instance, rootID string, fields []Field) (writeback.Root, error) {
	for _, r := range s.cfg.Policy.Roots {
		if r.Owner != owner || r.InstanceID != instance || r.RootID != rootID {
			continue
		}
		allowed := map[string]bool{}
		for _, f := range r.Fields {
			allowed[f] = true
		}
		for _, f := range fields {
			if !allowed[f.Name] {
				return r, domain.Fail("ACCESS_DENIED", "Writeback field is not authorized")
			}
		}
		return r, nil
	}
	return writeback.Root{}, domain.Fail("ACCESS_DENIED", "Writeback root is not authorized")
}

func (s *Service) Get(ctx context.Context, owner, id string) (Job, error) {
	j, err := s.repo.GetWriteback(ctx, owner, id)
	if err != nil || j.Status != "succeeded" {
		return j, err
	}
	r, err := s.journal.Load(ctx, id)
	if ctx.Err() != nil {
		return j, ctx.Err()
	}
	if err != nil || r.Stage != "committed" {
		j.Status = "recovery"
		j.Error = &domain.Error{Code: "TEMPORARY_FAILURE", Message: "Writeback journal requires recovery"}
	} else if s.lib.WritebackPending(owner, r.Target.InstanceID, id) {
		j.Status = "recovery"
		j.Error = &domain.Error{Code: "TEMPORARY_FAILURE", Message: "Writeback source protection is being released"}
	}
	if active := s.active.Load(); j.Status == "recovery" && active != nil && *active == id {
		// The durable DB result may precede the final journal flush and lease
		// release. An executing operation is still running, not interrupted.
		j.Status, j.Error = "running", nil
	}
	return j, nil
}
func (s *Service) Cancel(ctx context.Context, owner, id string) (Job, error) {
	return s.repo.CancelWriteback(ctx, owner, id)
}

func (s *Service) patch(ctx context.Context, w Work) (writeback.Patch, error) {
	p := writeback.Patch{Comments: map[string][]string{}, Overwrite: w.Job.Request.Overwrite}
	if p.Overwrite && !s.cfg.AllowOverwrite {
		return p, domain.Fail("ACCESS_DENIED", "Overwriting source tags is disabled")
	}
	for _, ref := range w.Objects {
		object, file, err := s.content.Open(ctx, w.Job.Owner, ref.ObjectID)
		if err != nil {
			return p, err
		}
		limit := int64(content.MaxLyricsBytes)
		if ref.Field.Name == "artwork" {
			limit = content.MaxArtworkBytes
		}
		data, e := io.ReadAll(io.LimitReader(file, limit+1))
		closeErr := file.Close()
		if e != nil {
			return p, e
		}
		if closeErr != nil {
			return p, closeErr
		}
		if int64(len(data)) > limit || object.Metadata.Field != ref.Field.Name || object.Metadata.Language != ref.Field.Language {
			return p, domain.Fail("RESOURCE_CHANGED", "Selected writeback content changed")
		}
		if ref.Field.Name == "artwork" {
			p.Artwork = data
		} else {
			p.Comments["lyrics"] = []string{string(data)}
		}
	}
	return p, nil
}

func (s *Service) Start(ctx context.Context) error {
	if err := s.recoverStartup(ctx); err != nil {
		return err
	}
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-ticker.C:
				s.runNext()
			}
		}
	}()
	return nil
}
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
}

func (s *Service) runNext() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runRecovery() {
		return
	}
	w, err := s.repo.NextWriteback(s.ctx)
	if err != nil {
		return
	}
	s.active.Store(&w.Job.ID)
	defer s.active.Store(nil)
	ctx, cancel := context.WithTimeout(s.ctx, 30*time.Minute)
	defer cancel()
	p, err := s.patch(ctx, w)
	if err == nil {
		_, err = s.editor.Apply(ctx, w.Job.Owner, writeback.Request{ID: w.Job.ID, Target: w.Target, SelectionRevision: w.Job.Request.ExpectedRevision, Patch: p})
	}
	if errors.Is(err, errBusy) {
		_ = s.repo.DeferWriteback(ctx, w.Job.Owner, w.Job.ID)
		return
	}
	s.settle(w.Job.Owner, w.Job.ID, w.Target, err)
}
