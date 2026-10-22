package audioffmpeg

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// EffectsDisabled 仅关闭听感效果；解码、声道转换与编码仍按输出格式执行。
func (p *Processor) EffectsDisabled() bool {
	return p != nil && p.config.DisableEffects
}

// WithoutEffects 创建无音效快照，不修改其他播放会话或共享的生命周期资源。
func (p *Processor) WithoutEffects() *Processor {
	if p == nil || p.config.DisableEffects {
		return p
	}
	config := p.config
	config.DisableEffects, config.DSPChain, config.DSPPlugins = true, nil, nil
	identity := p.identity
	identity.PipelineRevision = dspRevision(config, identity.WorkerSHA256)
	return &Processor{parent: p.runtime(), config: config, Capabilities: p.Capabilities,
		slots: p.slots, normalizeSlot: p.normalizeSlot, identity: identity}
}

func buildRequestedDSP(ctx context.Context, r StartRequest, format audio.Format) (*audio.Chain, error) {
	if !r.DisableEffects {
		return buildDSPFormat(ctx, r.Plan.GainDB, r.DSPChain, r.DSPPlugins, format)
	}
	// 空链不创建零增益节点，也不注册或启动任意效果插件。
	chain, err := audio.NewChain(audio.NewRegistry(), nil)
	if err != nil {
		return nil, err
	}
	if err = chain.Configure(format); err != nil {
		return nil, err
	}
	return chain, nil
}
