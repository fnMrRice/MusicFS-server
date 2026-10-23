package dspsettings

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspchain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

func validateShape(options Options) error {
	if options.BufferFrames < MinBufferFrames || options.BufferFrames > MaxBufferFrames {
		return domain.Fail("INVALID_ARGUMENT", "DSP bufferFrames must be between 64 and 16384")
	}
	if options.OutputLayout != "stereo" && options.OutputLayout != "5.1" && options.OutputLayout != "7.1" {
		return domain.Fail("INVALID_ARGUMENT", "DSP outputLayout must be stereo, 5.1 or 7.1")
	}
	if options.Chain == nil || len(options.Chain) >= audio.MaxChainNodes {
		return domain.Fail("INVALID_ARGUMENT", "DSP chain must be an array with at most 15 nodes")
	}
	for _, node := range options.Chain {
		if node.Name == "" || len(node.Name) > 64 || node.Version != audio.NodeVersion || len(node.Params) > 64 {
			return domain.Fail("INVALID_ARGUMENT", "Invalid DSP node name, version or parameter count")
		}
		for key, value := range node.Params {
			if key == "" || len(key) > 64 || math.IsNaN(value) || math.IsInf(value, 0) {
				return domain.Fail("INVALID_ARGUMENT", "Invalid DSP parameter")
			}
		}
	}
	return nil
}

func validate(ctx context.Context, cfg Config, options Options) (Preview, error) {
	if err := ctx.Err(); err != nil {
		return Preview{}, err
	}
	if err := validateShape(options); err != nil {
		return Preview{}, err
	}
	if cfg.DisableEffects && len(options.Chain) != 0 {
		return Preview{}, domain.Fail("UNSUPPORTED", "Server audio effects are disabled")
	}
	if cfg.DisableChannelMix {
		if options.OutputLayout != "stereo" {
			return Preview{}, domain.Fail("UNSUPPORTED", "Server channel mixing is disabled")
		}
		for _, node := range options.Chain {
			if audio.HasCapability(node.Name, "channelConversion") {
				return Preview{}, domain.Fail("UNSUPPORTED", "Server channel mixing is disabled")
			}
		}
	}
	if err := dspchain.Validate(options.Chain, cfg.Plugins); err != nil {
		return Preview{}, domain.Fail("INVALID_ARGUMENT", err.Error())
	}
	if !cfg.Available || !slices.Contains(cfg.OutputLayouts, options.OutputLayout) {
		return Preview{}, domain.Fail("UNSUPPORTED", "Requested DSP output is unavailable")
	}
	if cfg.Validate != nil {
		if err := cfg.Validate(ctx, options); err != nil {
			return Preview{}, domain.Fail("INVALID_ARGUMENT", err.Error())
		}
	}
	registry := audio.NewRegistry()
	if err := audio.RegisterDefinitions(registry); err != nil {
		return Preview{}, err
	}
	external := map[string]dspworker.Config{}
	for _, plugin := range cfg.Plugins {
		external[plugin.Name] = plugin
	}
	format, _ := audio.FormatForLayout(options.OutputLayout)
	preview := Preview{Input: format, Output: audio.Output{Format: format, CanContinueDSP: true}, Nodes: []Step{}}
	for index, spec := range options.Chain {
		if !preview.Output.CanContinueDSP {
			return Preview{}, domain.Fail("INVALID_ARGUMENT", fmt.Sprintf("DSP node %d follows an output that cannot continue DSP", index+1))
		}
		input := preview.Output.Format
		var output audio.Output
		var err error
		_, native := audio.Definition(spec.Name)
		if plugin, ok := external[spec.Name]; ok && (!plugin.Builtin || !native) {
			if input != audio.Canonical || options.BufferFrames > 4096 {
				return Preview{}, domain.Fail("INVALID_ARGUMENT", "External DSP v1 requires stereo and bufferFrames at most 4096")
			}
			output = audio.Output{Format: input, CanContinueDSP: true}
		} else {
			node, createErr := registry.CreateVersioned(spec)
			if createErr != nil {
				return Preview{}, domain.Fail("INVALID_ARGUMENT", createErr.Error())
			}
			output, err = audio.DescribeIO(node, input)
			node.Close()
			if err != nil {
				return Preview{}, domain.Fail("INVALID_ARGUMENT", err.Error())
			}
		}
		preview.Nodes = append(preview.Nodes, Step{Name: spec.Name, Input: input, Output: output})
		preview.Output = output
	}
	if preview.Output.Format != format {
		return Preview{}, domain.Fail("INVALID_ARGUMENT", "Final DSP output must match the selected output layout")
	}
	return preview, nil
}
