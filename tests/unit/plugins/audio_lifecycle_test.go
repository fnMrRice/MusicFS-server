package plugins_test

import (
	"context"
	"errors"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugincontrol"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
)

func TestAudioEntrypointsRespectDisabledGateBeforeExecution(t *testing.T) {
	gate := plugincontrol.NewGate(false)
	processor := &audioffmpeg.Processor{}
	processor.SetGate(gate)
	decoder := &ncmdecoder.Processor{}
	decoder.SetGate(gate)
	for name, call := range map[string]func() error{
		"process": func() error { return processor.Process(context.Background(), audioffmpeg.Plan{}, nil, 0, nil) },
		"normalize": func() error {
			_, err := processor.Normalize(context.Background(), audio.NormalizeRequest{}, nil, 0, nil)
			return err
		},
		"decrypt": func() error { _, err := decoder.Open(context.Background(), "", "", nil, 0); return err },
	} {
		t.Run(name, func(t *testing.T) {
			var failure *domain.Error
			if err := call(); !errors.As(err, &failure) || failure.Code != "PLUGIN_DISABLED" {
				t.Fatal("disabled plugin reached execution", err)
			}
			if gate.Active() != 0 {
				t.Fatal("disabled entrypoint retained lease")
			}
		})
	}
}

func TestAudioProcessorsRejectStaleGenerationAfterGateReopens(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	processor := &audioffmpeg.Processor{}
	decoder := &ncmdecoder.Processor{}
	processor.SetGate(gate)
	decoder.SetGate(gate)
	gate.SetAccepting(false, false)
	gate.Rotate()
	gate.SetAccepting(true, false)
	for _, acquire := range []func(context.Context) (*plugincontrol.Lease, error){processor.AcquireLease, decoder.AcquireLease} {
		lease, err := acquire(context.Background())
		if lease != nil {
			lease.Close()
		}
		var failure *domain.Error
		if !errors.As(err, &failure) || failure.Code != "RESOURCE_CHANGED" {
			t.Fatal("stale processor borrowed new generation admission", err)
		}
	}
	fresh := &audioffmpeg.Processor{}
	fresh.SetGate(gate)
	lease, err := fresh.AcquireLease(context.Background())
	if err != nil {
		t.Fatal("replacement processor not admitted", err)
	}
	lease.Close()
}
