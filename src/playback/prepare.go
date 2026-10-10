package playback

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/timeline"
)

// preparedInput owns an unpublished immutable snapshot and its source lease.
// The creation semaphore serializes all snapshot reservations, including
// recovery, so the old and new representations both count against the budget.
type preparedInput struct {
	service              *Service
	track                library.Track
	path, etag           string
	size                 int64
	reserved             int64
	lease                *library.ReadLease
	processed, decrypted bool
}

func (p *preparedInput) discard() {
	if p.lease != nil {
		p.lease.Close()
		p.lease = nil
	}
	retained := int64(0)
	if p.path != "" {
		if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
			// A failed unlink is still occupied cache space, even though this
			// representation never became visible to a playback session.
			p.service.mu.Lock()
			if _, exists := p.service.garbage[p.path]; !exists {
				p.service.garbage[p.path] = p.size
			}
			retained = p.size
			p.service.mu.Unlock()
		}
		p.path = ""
	}
	p.service.budget.Release(p.reserved - retained)
	p.reserved = 0
}
func (s *Service) prepare(ctx context.Context, owner, task string, r CreateRequest) (*preparedInput, error) {
	t, err := s.library.Track(ctx, owner, r.SourceTrackID)
	if err != nil {
		return nil, err
	}
	if t.Availability != "available" {
		return nil, domain.Fail("SOURCE_OFFLINE", "Track is unavailable")
	}
	if t.Size <= 0 {
		return nil, domain.Fail("UNSUPPORTED", "Source contains no audio data")
	}
	lease, err := s.library.HoldRead(ctx, owner, t.ProviderID)
	if err != nil {
		return nil, err
	}
	p := &preparedInput{service: s, track: t, lease: lease}
	ok := false
	defer func() {
		if !ok {
			p.discard()
		}
	}()
	defer s.library.Revoke(t.ProviderID, task)
	handle, err := s.library.Open(ctx, t, task)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	input, size, decrypted, closeInput, err := s.prepareInput(ctx, task, handle, &t)
	if err != nil {
		return nil, err
	}
	defer closeInput()
	if s.cache != nil {
		s.cache.MakeRoom(ctx, size)
	}
	if size <= 0 || !s.budget.TryAcquire(size) {
		return nil, domain.Fail("RATE_LIMITED", "Representation exceeds the cache budget")
	}
	p.reserved = size
	processed := r.Preferences.ForceProcessing || r.Preferences.GainDB != 0 || !originalCandidate(t) || !supports(r.Capabilities, t.MIME)
	if processed && (s.processor == nil || !supports(r.Capabilities, "audio/mpeg")) {
		return nil, domain.Fail("UNSUPPORTED", "No compatible audio processing profile is available")
	}
	path := filepath.Join(s.config.CacheRoot, task+".bin")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Cannot prepare playback representation")
	}
	p.path = path
	defer f.Close()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(preparationReader{ctx: ctx, input: input}, size))
	p.size = n
	if err != nil || n != size {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, domain.Fail("RESOURCE_CHANGED", "Source changed while preparing playback")
	}
	if err = handle.Validate(); err != nil {
		return nil, domain.Fail("RESOURCE_CHANGED", "Source changed while preparing playback")
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	if !processed && t.MIME == "audio/wav" {
		if err = inspectWave(p.path, &t); err != nil {
			return nil, err
		}
	}
	p.track, p.size, p.etag, p.processed, p.decrypted = t, n, fmt.Sprintf("\"%x\"", hash.Sum(nil)), processed, decrypted
	ok = true
	return p, nil
}

type preparationReader struct {
	ctx   context.Context
	input io.Reader
}

func (r preparationReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.input.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}
func (p *preparedInput) describe(id string, gain float64, expiry time.Time) Description {
	t := p.track
	d := Description{SessionID: id, AssetID: t.AssetID, DeliveryMode: "proxy", OutputProfile: OutputProfile{MIME: t.MIME, Codec: t.Codec, Container: t.Container, Transport: "htmlAudio"}, ExpiresAt: expiry, SeekMode: "byteRange", EffectiveProcessing: []Processing{}, ResumeDecision: "continue"}
	if p.decrypted {
		d.EffectiveProcessing = append(d.EffectiveProcessing, Processing{Type: "ncmDecrypt", Version: "1", Execution: "server"})
	}
	if t.DurationMs != nil {
		d.SeekableRange = &Range{EndMs: *t.DurationMs}
	}
	if p.processed {
		codec, container := "mp3", "mp3"
		d.DeliveryMode = "processed"
		d.OutputProfile = OutputProfile{MIME: "audio/mpeg", Codec: &codec, Container: &container, Transport: "htmlAudio"}
		d.SeekMode = "none"
		if t.DurationMs != nil {
			d.SeekMode = "timeRestart"
			d.SeekableRange.EndMs = min(d.SeekableRange.EndMs, timeline.MaxPositionMs)
		}
		d.EffectiveProcessing = append(d.EffectiveProcessing, Processing{Type: "gain", GainDB: gain, Version: "1", Execution: "server"})
	}
	return d
}
