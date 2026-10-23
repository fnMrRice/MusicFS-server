package bootstrap

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioruntime"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
	"io"
	"sync"
)

type probeRuntime struct {
	ffprobe              audioruntime.Probe
	decoder              audioruntime.Decoder
	audioGate, ncmGate   *plugincontrol.Gate
	audioEpoch, ncmEpoch uint64
}
type managedProbe struct {
	mu      sync.RWMutex
	current probeRuntime
}

func (p *managedProbe) update(ffprobe audioruntime.Probe, decoder audioruntime.Decoder, audioGate, ncmGate *plugincontrol.Gate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current = probeRuntime{ffprobe: ffprobe, decoder: decoder, audioGate: audioGate, ncmGate: ncmGate, audioEpoch: audioGate.Epoch(), ncmEpoch: ncmGate.Epoch()}
}
func (p *managedProbe) snapshot() probeRuntime { p.mu.RLock(); defer p.mu.RUnlock(); return p.current }
func (p probeRuntime) admit(ctx context.Context, encrypted bool) (context.Context, func(), error) {
	leases := []*plugincontrol.Lease{}
	closeAll := func() {
		for i := len(leases) - 1; i >= 0; i-- {
			leases[i].Close()
		}
	}
	add := func(gate *plugincontrol.Gate, epoch uint64) error {
		lease, err := gate.AcquireEpoch(ctx, epoch)
		if err != nil {
			return err
		}
		if lease != nil {
			ctx = lease.Context()
			leases = append(leases, lease)
		}
		return nil
	}
	if p.ffprobe != nil {
		if err := add(p.audioGate, p.audioEpoch); err != nil {
			closeAll()
			return ctx, func() {}, err
		}
	}
	if encrypted && p.decoder != nil {
		if err := add(p.ncmGate, p.ncmEpoch); err != nil {
			closeAll()
			return ctx, func() {}, err
		}
	}
	return ctx, closeAll, nil
}
func (p *managedProbe) probe(ctx context.Context, input io.ReadSeeker, size int64) (library.Observation, error) {
	runtime := p.snapshot()
	return runtime.probe(ctx, input, size)
}
func (p *managedProbe) writeProbe(ctx context.Context, input io.ReadSeeker, size int64) (library.Observation, error) {
	runtime := p.snapshot()
	if runtime.ffprobe == nil {
		return library.Observation{}, domain.Fail("UNSUPPORTED", "Write-back validation requires an available audio probe")
	}
	return runtime.probe(ctx, input, size)
}
func (runtime probeRuntime) probe(ctx context.Context, input io.ReadSeeker, size int64) (library.Observation, error) {
	wrapped := false
	if reader, ok := input.(io.ReaderAt); ok {
		match, matched, err := audiocodecs.Detect(reader, size)
		if err != nil {
			return library.Observation{}, err
		}
		wrapped = matched && match.RequiresDecode
	}
	ctx, release, err := runtime.admit(ctx, wrapped)
	if err != nil {
		return library.Observation{}, err
	}
	defer release()
	probe := metadataProbe(runtime.ffprobe, runtime.decoder)
	if probe == nil {
		return library.Observation{}, domain.Fail("UNSUPPORTED", "Metadata probing is unavailable")
	}
	return probe(ctx, input, size)
}

// A scan owns a fixed dependency set for its entire bounded scope. Draining an
// audio plugin cannot turn remaining files into successful empty metadata.
func (p *managedProbe) session(ctx context.Context) (context.Context, library.Probe, func(), error) {
	runtime := p.snapshot()
	ctx, release, err := runtime.admit(ctx, true)
	if err != nil {
		return ctx, nil, release, err
	}
	return ctx, metadataProbe(runtime.ffprobe, runtime.decoder), release, nil
}

func (p *managedProbe) ready() bool {
	r := p.snapshot()
	return r.ffprobe != nil && r.audioGate.Check(context.Background()) == nil
}

// Write-back must reserve validation tools before touching any original bytes.
func (p *managedProbe) writeSession(ctx context.Context) (context.Context, library.Probe, func(), error) {
	r := p.snapshot()
	if r.ffprobe == nil {
		return ctx, nil, func() {}, domain.Fail("UNSUPPORTED", "Write-back validation requires an available audio probe")
	}
	ctx, release, err := r.admit(ctx, false)
	if err != nil {
		return ctx, nil, release, err
	}
	return ctx, r.probe, release, nil
}
