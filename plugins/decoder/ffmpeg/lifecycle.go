package audioffmpeg

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

type lifecycleBinding struct {
	gate  *plugincontrol.Gate
	epoch uint64
}

// SetGate is safe during bootstrap attachment and replaces no processing state.
func (p *Processor) SetGate(gate *plugincontrol.Gate) {
	if p != nil {
		if gate == nil {
			p.gate.Store(nil)
		} else {
			p.gate.Store(&lifecycleBinding{gate, gate.Epoch()})
		}
	}
}

func (p *Processor) AcquireLease(ctx context.Context) (*plugincontrol.Lease, error) {
	if p == nil {
		return nil, nil
	}
	binding := p.gate.Load()
	if binding == nil {
		return nil, nil
	}
	return binding.gate.AcquireEpoch(ctx, binding.epoch)
}
