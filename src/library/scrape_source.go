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
	audio, err := s.Open(ctx, t, task)
	if err != nil {
		return nil, err
	}
	ctx = s.taskContext(t.ProviderID, task, ctx)
	h.ctx = ctx
	guard, err := audio.Stable(ctx)
	audio.Close()
	if err != nil {
		return nil, err
	}
	h.guards = append(h.guards, guard)
	ref, err := newScrapeRef()
	if err != nil {
		return nil, err
	}
	h.Inputs = append(h.Inputs, enrichment.Input{Resource: enrichment.Resource{ResourceRef: ref, Version: t.Version, Kind: "audio", SizeBytes: t.Size, Match: "embedded"}, Reader: guard, Validate: h.Validate})
	match, matched, err := audiocodecs.Detect(guard, t.Size)
	if err != nil {
		return nil, err
	}
	h.Encrypted = matched && match.RequiresDecode
	scope := sourceaccess.Scope{UserID: owner, InstanceID: t.ProviderID, TaskID: task, AssetID: t.AssetID}
	runtime, ok := s.Runtime(t.ProviderID)
	if !ok || runtime.Worker == nil {
		return nil, domain.Fail("SOURCE_OFFLINE", "Provider is offline")
	}
	access := runtime.Worker.Access
	sidecars, err := access.AuthorizeSidecars(ctx, scope, t.RootID, t.Path, t.Version, fields)
	if err != nil {
		return nil, err
	}
	var sidecarBytes int64
	for _, sidecar := range sidecars {
		r := sidecar.Resource
		if r.SizeBytes > (32<<20)-sidecarBytes {
			h.Issues = append(h.Issues, enrichment.Issue{ResourceRef: r.ResourceRef, Code: "UNSUPPORTED"})
			continue
		}
		file, err := access.Open(ctx, scope, r.ResourceRef)
		if err != nil {
			h.Issues = append(h.Issues, enrichment.Issue{ResourceRef: r.ResourceRef, Code: "RESOURCE_CHANGED"})
			continue
		}
		stable, err := file.Stable(ctx)
		file.Close()
		if err != nil {
			h.Issues = append(h.Issues, enrichment.Issue{ResourceRef: r.ResourceRef, Code: "UNSUPPORTED"})
			continue
		}
		h.guards = append(h.guards, stable)
		sidecarBytes += r.SizeBytes
		h.Inputs = append(h.Inputs, enrichment.Input{Resource: enrichment.Resource{ResourceRef: r.ResourceRef, Version: r.ObservedVersion, Kind: sidecar.Kind, SizeBytes: r.SizeBytes, Match: sidecar.Match}, Reader: stable, Validate: h.Validate})
	}
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
