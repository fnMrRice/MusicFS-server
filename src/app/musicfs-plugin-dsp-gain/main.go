package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/audio/dsp"
	"github.com/fnMrRice/MusicFS-server/src/audio/dspworker"
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
	if err := dspworker.Serve(ctx, os.Stdin, os.Stdout, "musicfs.dsp.gain", factory); err != nil {
		fmt.Fprintln(os.Stderr, "DSP worker failed:", err)
		os.Exit(1)
	}
}
