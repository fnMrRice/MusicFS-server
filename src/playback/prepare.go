package playback

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
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
	processor            audioruntime.Processor
	profile              audiotypes.Profile
	decoder              audioruntime.Decoder
	decodeProcessing     []audioformat.Processing
	processing           *plugincontrol.Lease
	decoding             []*plugincontrol.Lease
	decoderLeases        map[string]*plugincontrol.Lease
	decodeOutput         string
	dspExecution         string
	gain                 float64
	online               bool
	network              networkInput
}

func (p *preparedInput) discard() {
	p.processing.Close()
	for _, lease := range p.decoding {
		lease.Close()
	}
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
func (s *Service) prepare(ctx context.Context, owner, task string, r CreateRequest, fixed ...audioruntime.Processor) (*preparedInput, error) {
	t, err := s.library.Track(ctx, owner, r.SourceTrackID)
	if err != nil {
		return nil, err
	}
	if t.Availability != "available" {
		return nil, domain.Fail("SOURCE_OFFLINE", "Track is unavailable")
	}
	if s.library.SourceType(t.ProviderID) == "network" {
		return s.prepareOnline(ctx, owner, t, r)
	}
	if s.library.SourceType(t.ProviderID) != "local" {
		return nil, domain.Fail("UNSUPPORTED", "Provider has not declared a supported source type")
	}
	// 扫描元数据已绑定资产版本；NCM 为解封后的音频码率，不能改用外层大小或中间 PCM 码率。
	var sourceBitrate int64
	if t.BitrateBps != nil {
		sourceBitrate = *t.BitrateBps
	}
	lease, err := s.library.HoldReadLifetime(ctx, s.ctx, owner, t.ProviderID)
	if err != nil {
		return nil, err
	}
	p := &preparedInput{service: s, track: t, lease: lease, decodeOutput: "native", dspExecution: r.Preferences.DSPExecution, gain: r.Preferences.GainDB}
	p.processor, p.decoder = s.audioSnapshot()
	if len(fixed) > 0 {
		p.processor = fixed[0]
	} else {
		p.processor, err = s.processorForUser(ctx, owner, p.processor, r.Preferences.DSPExecution == "client")
		if err != nil {
			p.discard()
			return nil, err
		}
	}
	if r.Preferences.DSPExecution == "client" || s.config.DisableEffects || p.processor.EffectsDisabled() {
		p.gain = 0
	}
	ctx, cancelPreparation := context.WithCancel(lease.WithContext(ctx))
	unlink := context.AfterFunc(lease.Context(), cancelPreparation)
	defer func() { unlink(); cancelPreparation() }()
	ok := false
	defer func() {
		if !ok {
			p.discard()
		}
	}()
	defer s.library.Revoke(t.ProviderID, task)
	handle, err := s.library.OpenPlayback(ctx, t, task)
	if err != nil {
		return nil, err
	}
	defer handle.Close()
	t.Size = handle.Entry.SizeBytes
	if t.Size <= 0 {
		return nil, domain.Fail("UNSUPPORTED", "Source contains no audio data")
	}
	input, size, decrypted, closeInput, err := p.prepareInput(ctx, task, handle, &t)
	if err != nil {
		return nil, err
	}
	defer closeInput()
	slog.DebugContext(ctx, "Playback input prepared", "sessionId", task, "instanceId", t.ProviderID, "sizeBytes", size, "decoded", decrypted)
	if s.cache != nil {
		s.cache.MakeRoom(ctx, size)
	}
	if size <= 0 || !s.budget.TryAcquire(size) {
		return nil, domain.Fail("RATE_LIMITED", "Representation exceeds the cache budget")
	}
	p.reserved = size
	processed := p.decodeOutput != "native" || s.config.RequireDSP && r.Preferences.DSPExecution != "client" || r.Preferences.ForceProcessing || p.gain != 0 || p.processor.HasDSP() || p.processor.RequiresChannelConversion() || !originalCandidate(t) || !supports(r.Capabilities, t.MIME)
	if processed {
		if p.processing == nil {
			ctx, err = p.processingLease(ctx)
			if err != nil {
				return nil, err
			}
		}
		if p.processing != nil {
			stop := context.AfterFunc(p.processing.Context(), cancelPreparation)
			defer stop()
		}
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
		if leaseErr := p.checkLeases(); leaseErr != nil {
			return nil, leaseErr
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
	if err = p.checkLeases(); err != nil {
		return nil, err
	}
	if !processed {
		if err = p.inspectOriginalProfile(ctx, &t); err != nil {
			return nil, err
		}
	}
	if err = p.selectProcessingProfile(ctx, r.Capabilities, sourceBitrate, processed, len(fixed) > 0); err != nil {
		return nil, err
	}
	p.track, p.size, p.etag, p.processed, p.decrypted = t, n, fmt.Sprintf("\"%x\"", hash.Sum(nil)), processed, decrypted
	ok = true
	if slog.Default().Enabled(ctx, slog.Level(-8)) {
		slog.Log(ctx, slog.Level(-8), "Playback representation prepared", "sessionId", task, "sizeBytes", n, "processed", processed)
	}
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
func (p *preparedInput) describe(id string, gain float64, expiry time.Time, transport string) Description {
	t := p.track
	if p.online {
		d := p.describeOnline(id, expiry, transport)
		d.OutputProfilePending = d.OutputProfile.Channels == 0
		return d
	}
	d := Description{SessionID: id, SourceTrackID: t.ID, AssetID: t.AssetID, DeliveryMode: "proxy", OutputProfile: OutputProfile{MIME: t.MIME, Codec: t.Codec, Container: t.Container, Transport: transport,
		SampleRate: p.profile.SampleRate, Channels: p.profile.Channels, Layout: p.profile.Layout, BitrateBps: p.profile.BitrateBps}, ExpiresAt: expiry, SeekMode: "byteRange", EffectiveProcessing: []Processing{}, ResumeDecision: "continue"}
	d.DSPExecution = p.dspExecution
	for _, step := range p.decodeProcessing {
		d.EffectiveProcessing = append(d.EffectiveProcessing, Processing{Type: step.Type, Version: step.Version, Execution: "server"})
	}
	if t.DurationMs != nil {
		d.SeekableRange = &Range{EndMs: *t.DurationMs}
	}
	if p.processed {
		profile := p.profile
		d.DeliveryMode = "processed"
		d.OutputProfile = OutputProfile{ID: profile.ID, MIME: profile.MIME, Codec: &profile.Codec, Container: &profile.Container, Transport: transport,
			SampleRate: profile.SampleRate, Channels: profile.Channels, Layout: profile.Layout, BitrateBps: profile.BitrateBps}
		d.SeekMode = "none"
		if t.DurationMs != nil {
			d.SeekMode = "timeRestart"
			d.SeekableRange.EndMs = min(d.SeekableRange.EndMs, timeline.MaxPositionMs)
		}
		if !p.processor.ChannelMixDisabled() {
			d.EffectiveProcessing = append(d.EffectiveProcessing,
				Processing{Type: "channelMix", Version: p.processor.DSPPluginVersion("channelConversion"), Execution: "server", Parameters: map[string]float64{"outputChannels": float64(profile.Channels)}})
		}
		if !p.processor.EffectsDisabled() {
			d.EffectiveProcessing = append(d.EffectiveProcessing, Processing{Type: "gain", GainDB: gain, Version: "1", Execution: "server"})
		}
		for _, node := range p.processor.DSPChain() {
			step := Processing{Type: node.Name, Version: strconv.Itoa(node.Version), Execution: "server", Parameters: node.Params}
			if audio.HasCapability(node.Name, "gain") {
				step.GainDB = node.Params["gainDB"]
			}
			d.EffectiveProcessing = append(d.EffectiveProcessing, step)
		}
	}
	return d
}
