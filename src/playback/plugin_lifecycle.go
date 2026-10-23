package playback

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"

	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

// UpdateAudio affects new sessions only. Existing sessions retain their verified
// processor and admitted leases until stopping or explicit source recovery.
func (s *Service) UpdateAudio(processor audioruntime.Processor, decoder audioruntime.Decoder) {
	s.mu.Lock()
	s.processor, s.decoder = audioruntime.Safe(processor), decoder
	s.mu.Unlock()
}

func (s *Service) audioSnapshot() (audioruntime.Processor, audioruntime.Decoder) {
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

func (p *preparedInput) decoderLease(ctx context.Context, decoder audioruntime.Decoder, request audioformat.Request, input audioformat.Input) (context.Context, error) {
	id := decoder.AudioDecoder().PluginID
	if id == p.processor.AudioDecoder().PluginID {
		if p.processing != nil {
			return plugincontrol.WithLease(ctx, p.processing), nil
		}
		return p.processingLease(ctx)
	}
	if p.decoderLeases == nil {
		p.decoderLeases = map[string]*plugincontrol.Lease{}
	}
	if lease, ok := p.decoderLeases[id]; ok {
		return plugincontrol.WithLease(ctx, lease), nil
	}
	lease, err := decoder.AcquireLease(context.WithoutCancel(ctx))
	if err != nil {
		return ctx, err
	}
	p.decoderLeases[id] = lease
	p.decoding = append(p.decoding, lease)
	return plugincontrol.WithLease(ctx, lease), nil
}

func (p *preparedInput) checkLeases() error {
	if p.lease != nil {
		if err := p.lease.Context().Err(); err != nil {
			return err
		}
	}
	for _, lease := range append([]*plugincontrol.Lease{p.processing}, p.decoding...) {
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
	a.decodeOutput = p.decodeOutput
	a.ctx, a.cancel = context.WithCancel(s.ctx)
	if p.lease != nil {
		a.ctx = p.lease.WithContext(a.ctx)
		a.unlinkPlugins = append(a.unlinkPlugins, context.AfterFunc(p.lease.Context(), a.cancel))
	}
	for _, lease := range append([]*plugincontrol.Lease{a.processing}, a.decoding...) {
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
	for _, lease := range a.decoding {
		lease.Close()
	}
	a.processing, a.decoding = nil, nil
}
