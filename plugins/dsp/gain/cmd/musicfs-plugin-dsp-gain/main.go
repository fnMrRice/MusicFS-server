package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	factory := func(params map[string]float64) (audio.Node, error) {
		registry := audio.NewRegistry()
		if err := dsp.Register(registry); err != nil {
			return nil, err
		}
		return registry.Create("gain", params)
	}
	if err := dspworker.Serve(ctx, os.Stdin, os.Stdout, "musicfs.dsp.gain", bundledplugin.GainVersion, factory); err != nil {
		fmt.Fprintln(os.Stderr, "DSP worker failed:", err)
		os.Exit(1)
	}
}
