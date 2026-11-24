package library

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

// ScrapeInput is a host-only, single-task bundle. Set Target.Revision from the
// content selection before opening a scraper session; it is not an asset version.
// 并发读取前必须完成 Inputs 和 DecodeAudio 准备。
type ScrapeInput struct {
	Target    enrichment.Target
	Inputs    []enrichment.Input
	Issues    []enrichment.Issue
	Query     enrichment.Query
	Encrypted bool

	service      *Service
	track        Track
	task         string
	ctx          context.Context
	cancel       context.CancelFunc
	guards       []*sourceaccess.StableReader
	decoded      *audioformat.Decoded
	decodeCancel context.CancelFunc
	mu           sync.Mutex
	closed       bool
	tried        bool
	fields       []string
}

// OpenScrape grants only the current owner's audio and exact selected siblings.
// Close must run after UploadGuarded finishes; the whole bundle expires in 30s.
func (s *Service) OpenScrape(ctx context.Context, owner, task, trackID string, fields []string) (*ScrapeInput, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if owner == "" || task == "" || len(task) > 128 || len(fields) < 1 || len(fields) > 2 {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid scraper source request")
	}
	for i, field := range fields {
		if (field != "lyrics" && field != "artwork") || slices.Contains(fields[:i], field) {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid scraper fields")
		}
	}
	t, err := s.scrapeTrack(ctx, owner, trackID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	h := &ScrapeInput{Target: enrichment.Target{SourceTrackID: t.ID, AssetID: t.AssetID, AssetVersion: t.Version},
		Inputs: []enrichment.Input{}, Issues: []enrichment.Issue{}, service: s, track: t, task: task, ctx: ctx, cancel: cancel,
		Query: enrichment.Query{Artists: append([]string{}, t.Artists...)}, fields: slices.Clone(fields)}
	if t.Title != nil {
		h.Query.Title = *t.Title
	}
	success := false
	defer func() {
		if !success {
			h.Close()
		}
	}()
	if err = h.Target.Validate(); err != nil {
		return nil, err
	}
	lease, err := s.holdTask(ctx, owner, t.ProviderID, task)
	if err != nil {
		return nil, err
	}
	if lease.runtime.Worker == nil || lease.runtime.Worker.Access == nil {
		return nil, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	h.ctx = lease.Context()
	scope := sourceaccess.Scope{UserID: owner, InstanceID: t.ProviderID, TaskID: task, AssetID: t.AssetID}
	files, err := lease.runtime.Worker.Access.OpenScrapeFiles(h.ctx, scope, t.RootID, t.Path, t.Version, t.Size, fields)
	if err != nil {
		return nil, err
	}
	// 先接管全部读取保护，构造任一输入失败时由 Close 统一释放。
	for _, file := range files {
		if file.Reader != nil {
			h.guards = append(h.guards, file.Reader)
		}
	}
	for _, file := range files {
		ref, err := newScrapeRef()
		if err != nil {
			return nil, err
		}
		if file.Issue != "" {
			h.Issues = append(h.Issues, enrichment.Issue{ResourceRef: ref, Code: file.Issue})
			continue
		}
		resource := enrichment.Resource{ResourceRef: ref, Version: file.Version, Kind: file.Kind, SizeBytes: file.SizeBytes, Match: file.Match}
		h.Inputs = append(h.Inputs, enrichment.Input{Resource: resource, Reader: file.Reader, Validate: h.Validate})
	}
	match, matched, err := audiocodecs.Detect(h.Inputs[0].Reader, t.Size)
	if err != nil {
		return nil, err
	}
	h.Encrypted = matched && match.RequiresDecode
	if err = h.Validate(); err != nil {
		return nil, err
	}
	success = true
	return h, nil
}

func (s *Service) scrapeTrack(ctx context.Context, owner, id string) (Track, error) {
	if err := ctx.Err(); err != nil {
		return Track{}, err
	}
	t, err := s.Track(ctx, owner, id)
	if err != nil {
		return Track{}, err
	}
	r, ok := s.Runtime(t.ProviderID)
	if !ok || r.Owner != owner || t.Owner != owner {
		return Track{}, domain.Fail("NOT_FOUND", "Source track not found")
	}
	if err := s.checkProviderWork(ctx, r); err != nil {
		return Track{}, err
	}
	if r.Worker == nil || r.Worker.Access == nil {
		return Track{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	select {
	case <-r.Worker.Done():
		return Track{}, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	default:
	}
	if t.Availability != "available" || t.Asset == nil || !t.Asset.Current || t.Asset.ID != t.AssetID || t.Asset.Version != t.Version || t.Asset.FileSizeBytes == nil || *t.Asset.FileSizeBytes != t.Size || t.Size <= 0 {
		return Track{}, domain.Fail("RESOURCE_CHANGED", "Source asset is no longer current")
	}
	return t, nil
}

func (h *ScrapeInput) current() error {
	t, err := h.service.scrapeTrack(h.ctx, h.track.Owner, h.track.ID)
	if err != nil {
		return err
	}
	if t.AssetID != h.track.AssetID || t.Version != h.track.Version || t.Size != h.track.Size || t.ProviderID != h.track.ProviderID || t.RootID != h.track.RootID || t.Path != h.track.Path {
		return domain.Fail("RESOURCE_CHANGED", "Scraper source asset changed")
	}
	return nil
}

func (h *ScrapeInput) Validate() error {
	if err := h.ctx.Err(); err != nil {
		return err
	}
	for _, guard := range h.guards {
		if err := guard.Validate(); err != nil {
			return err
		}
	}
	return h.current()
}

// Publish retains every file protection through the short reference transaction.
// Do not call Validate or Close from publish: the source guards are already held.
func (h *ScrapeInput) Publish(publish func() error) error {
	if publish == nil {
		return domain.Fail("INVALID_ARGUMENT", "Scraper publication callback is required")
	}
	var hold func(int) error
	hold = func(index int) error {
		if index < len(h.guards) {
			return h.guards[index].Publish(func() error { return hold(index + 1) })
		}
		if err := h.current(); err != nil {
			return err
		}
		return publish()
	}
	return hold(0)
}

func (h *ScrapeInput) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.closed = true
	h.cancel()
	if h.decodeCancel != nil {
		h.decodeCancel()
	}
	var err error
	if h.decoded != nil {
		err = h.decoded.Close()
	}
	for _, guard := range h.guards {
		err = errors.Join(err, guard.Close())
	}
	h.service.Revoke(h.track.ProviderID, h.task)
	return err
}

func newScrapeRef() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", domain.Fail("TEMPORARY_FAILURE", "Scraper resource identity unavailable")
	}
	return hex.EncodeToString(value[:]), nil
}
