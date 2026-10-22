package audioffmpeg

import (
	"context"
	"fmt"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/dspsettings"
)

const DefaultBufferFrames = 4096

func (p *Processor) DSPPlugins() []dspworker.Config {
	if p == nil {
		return nil
	}
	result := make([]dspworker.Config, len(p.config.DSPPlugins))
	for i, plugin := range p.config.DSPPlugins {
		result[i] = plugin.Clone()
	}
	return result
}

func effectiveBufferFrames(value int) int {
	if value == 0 {
		return DefaultBufferFrames
	}
	return value
}

func bufferFrames(value int) (int, error) {
	value = effectiveBufferFrames(value)
	if value < 64 || value > 16384 {
		return 0, fmt.Errorf("INVALID_ARGUMENT: DSP buffer frames must be between 64 and 16384")
	}
	return value, nil
}

// runtime 返回共享的生命周期与并发资源，不复制原子状态或同步对象。
func (p *Processor) runtime() *Processor {
	for p.parent != nil {
		p = p.parent
	}
	return p
}

func (p *Processor) Defaults() dspsettings.Options {
	value := dspsettings.Options{Chain: []audio.NodeSpec{}, OutputLayout: "stereo", BufferFrames: DefaultBufferFrames}
	if p != nil {
		value.Chain = p.DSPChain()
		if value.Chain == nil {
			value.Chain = []audio.NodeSpec{}
		}
		if p.config.OutputLayout != "" {
			value.OutputLayout = p.config.OutputLayout
		}
		value.BufferFrames = effectiveBufferFrames(p.config.BufferFrames)
	}
	return value
}

func (p *Processor) AvailableOutputLayouts() []string {
	result := []string{}
	if p == nil {
		return result
	}
	if p.config.DisableChannelMix {
		return []string{"stereo"}
	}
	for _, candidate := range p.Capabilities.Profiles {
		if profile, ok := ProfileByID(candidate.ID); ok && (p.config.Builtin || profile.Layout == "stereo") {
			result = append(result, profile.Layout)
		}
	}
	return result
}

// ValidateSettings 仅检查已探测的部署能力与节点声明，不启动外部插件。
func (p *Processor) ValidateSettings(ctx context.Context, options dspsettings.Options) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p == nil {
		return fmt.Errorf("UNSUPPORTED: audio processor is unavailable")
	}
	if p.config.DisableEffects && len(options.Chain) != 0 {
		return fmt.Errorf("UNSUPPORTED: server audio effects are disabled")
	}
	if p.config.DisableChannelMix && (options.OutputLayout != "stereo" || len(WithoutChannelMixNodes(options.Chain)) != len(options.Chain)) {
		return fmt.Errorf("UNSUPPORTED: server channel mixing is disabled")
	}
	if options.BufferFrames == 0 {
		return fmt.Errorf("INVALID_ARGUMENT: DSP buffer frames must be specified")
	}
	if _, err := bufferFrames(options.BufferFrames); err != nil {
		return err
	}
	config := p.config
	config.DSPChain, config.OutputLayout, config.BufferFrames = options.Chain, options.OutputLayout, options.BufferFrames
	if err := validateExecution(config); err != nil {
		return err
	}
	if err := ValidateDSP(options.Chain, config.DSPPlugins); err != nil {
		return err
	}
	view := &Processor{config: config, Capabilities: p.Capabilities}
	profile, err := view.OutputProfile()
	if err != nil || options.OutputLayout == "" {
		return fmt.Errorf("UNSUPPORTED: selected output layout is unavailable")
	}
	format, err := audio.FormatForLayout(profile.Layout)
	if err != nil {
		return err
	}
	registry := audio.NewRegistry()
	if err = builtin.Register(registry); err != nil {
		return err
	}
	for _, plugin := range config.DSPPlugins {
		if err = registry.Register(plugin.Name, func(params map[string]float64) (audio.Node, error) {
			if plugin.Builtin {
				return registry.Create("gain", params)
			}
			if options.BufferFrames > 4096 {
				return nil, fmt.Errorf("UNSUPPORTED: external DSP v1 allows at most 4096 buffer frames")
			}
			return externalContract{}, nil
		}); err != nil {
			return err
		}
	}
	output, err := audio.NegotiateChain(registry, options.Chain, format)
	if err != nil {
		return err
	}
	if output.Format != format {
		return fmt.Errorf("UNSUPPORTED: DSP output differs from selected encoding layout")
	}
	return nil
}

// WithSettings 创建不可变的播放配置视图；所有用户共享原实例的门禁和任务配额。
func (p *Processor) WithSettings(ctx context.Context, options dspsettings.Options) (*Processor, error) {
	if err := p.ValidateSettings(ctx, options); err != nil {
		return nil, err
	}
	config := p.config
	config.DSPChain = CloneDSPChain(options.Chain)
	config.OutputLayout, config.BufferFrames = options.OutputLayout, options.BufferFrames
	identity := p.identity
	identity.PipelineRevision = dspRevision(config, identity.WorkerSHA256)
	return &Processor{parent: p.runtime(), config: config, Capabilities: p.Capabilities, dspVersions: p.dspVersions,
		slots: p.slots, normalizeSlot: p.normalizeSlot, identity: identity}, nil
}

// DeploymentIdentity 不包含用户覆盖，用于阻止替换部署后复用旧时间轴证据。
func (p *Processor) DeploymentIdentity() ProcessingIdentity {
	if p == nil {
		return ProcessingIdentity{}
	}
	return p.runtime().identity
}

// 外部 v1 声明固定立体声，无状态、不改变帧和声道格式。
type externalContract struct{}

func (externalContract) DescribeIO(format audio.Format) (audio.Output, error) {
	if format != audio.Canonical {
		return audio.Output{}, fmt.Errorf("UNSUPPORTED: external DSP v1 requires stereo output and input")
	}
	return audio.Output{Format: format, CanContinueDSP: true}, nil
}
func (externalContract) Configure(audio.Format) error { return nil }
func (externalContract) Process(*audio.Block) error   { return nil }
func (externalContract) Reset() error                 { return nil }
func (externalContract) Drain() error                 { return nil }
func (externalContract) Close() error                 { return nil }
func (externalContract) DelayFrames() int64           { return 0 }
