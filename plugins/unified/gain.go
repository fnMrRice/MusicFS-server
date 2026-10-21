package unified

import (
	"context"
	"io"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
)

func runGain(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
	factory := func(params map[string]float64) (audio.Node, error) {
		registry := audio.NewRegistry()
		if err := dsp.Register(registry); err != nil {
			return nil, err
		}
		return registry.Create("gain", params)
	}
	return dspworker.Serve(ctx, input, output, "musicfs.dsp.gain", bundledplugin.GainVersion, factory)
}
