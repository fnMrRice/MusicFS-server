package playback

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
)

// UpdateAudio affects new sessions only. Existing sessions retain their verified
// processor and admitted leases until stopping or explicit source recovery.
func (s *Service) UpdateAudio(processor *audioffmpeg.Processor, decoder *ncmdecoder.Processor) {
	s.mu.Lock()
	s.processor, s.decoder = processor, decoder
	s.mu.Unlock()
}

func (s *Service) audioSnapshot() (*audioffmpeg.Processor, *ncmdecoder.Processor) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.processor, s.decoder
}

func (p *preparedInput) processingLease(ctx context.Context) (context.Context, error) {
	if p.processor == nil {
		return ctx, nil
	}
	lease, err := p.processor.AcquireLease(context.WithoutCancel(ctx))
	if err != nil {
		return ctx, err
	}
	p.processing = lease
	return plugincontrol.WithLease(ctx, lease), nil
}

func (p *preparedInput) decoderLease(ctx context.Context) (context.Context, error) {
	if p.decoder == nil {
		return ctx, nil
	}
	lease, err := p.decoder.AcquireLease(context.WithoutCancel(ctx))
	if err != nil {
		return ctx, err
	}
	p.decoding = lease
	return plugincontrol.WithLease(ctx, lease), nil
}

func (p *preparedInput) checkLeases() error {
	if p.lease != nil {
		if err := p.lease.Context().Err(); err != nil {
			return err
		}
	}
	for _, lease := range []*plugincontrol.Lease{p.processing, p.decoding} {
		if lease != nil {
			if err := lease.Context().Err(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) installPluginLeases(a *session, p *preparedInput) {
	a.processor, a.processing, a.decoding = p.processor, p.processing, p.decoding
	a.ctx, a.cancel = context.WithCancel(s.ctx)
	if p.lease != nil {
		a.ctx = p.lease.WithContext(a.ctx)
		a.unlinkPlugins = append(a.unlinkPlugins, context.AfterFunc(p.lease.Context(), a.cancel))
	}
	for _, lease := range []*plugincontrol.Lease{a.processing, a.decoding} {
		if lease == nil {
			continue
		}
		a.ctx = plugincontrol.WithLease(a.ctx, lease)
		a.unlinkPlugins = append(a.unlinkPlugins, context.AfterFunc(lease.Context(), a.cancel))
	}
}

func (a *session) releasePluginLeases() {
	for _, unlink := range a.unlinkPlugins {
		unlink()
	}
	a.unlinkPlugins = nil
	a.processing.Close()
	a.decoding.Close()
	a.processing, a.decoding = nil, nil
}
