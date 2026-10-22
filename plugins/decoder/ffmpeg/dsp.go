package audioffmpeg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspchain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

// ValidateDSP 校验配置，不打开文件或启动插件。
func ValidateDSP(chain []audio.NodeSpec, plugins []dspworker.Config) error {
	return dspchain.Validate(chain, plugins)
}

func CloneDSPChain(chain []audio.NodeSpec) []audio.NodeSpec {
	return dspchain.Clone(chain)
}

func (p *Processor) HasDSP() bool { return p != nil && len(p.config.DSPChain) != 0 }

// DSPPluginVersion 返回初始化时装配的内置发行号或握手验证过的外部发行号。
// 查询不会启动空闲节点，也不会访问活动播放状态。
func (p *Processor) DSPPluginVersion(name string) string {
	if p == nil {
		return ""
	}
	if version := builtin.Version(name); version != "" {
		return version
	}
	return p.dspVersions[name]
}

func (p *Processor) DSPChain() []audio.NodeSpec {
	if p == nil {
		return nil
	}
	return CloneDSPChain(p.config.DSPChain)
}

func buildDSP(ctx context.Context, gain float64, chain []audio.NodeSpec, plugins []dspworker.Config) (*audio.Chain, error) {
	return buildDSPWithVersions(ctx, gain, chain, plugins, nil)
}

func buildDSPWithVersions(ctx context.Context, gain float64, chain []audio.NodeSpec, plugins []dspworker.Config, versions map[string]string) (*audio.Chain, error) {
	return buildDSPFormatVersions(ctx, gain, chain, plugins, audio.Canonical, versions)
}

func buildDSPFormat(ctx context.Context, gain float64, chain []audio.NodeSpec, plugins []dspworker.Config, format audio.Format) (*audio.Chain, error) {
	return buildDSPFormatVersions(ctx, gain, chain, plugins, format, nil)
}

func buildDSPFormatVersions(ctx context.Context, gain float64, chain []audio.NodeSpec, plugins []dspworker.Config, format audio.Format, versions map[string]string) (*audio.Chain, error) {
	if err := format.Validate(); err != nil {
		return nil, err
	}
	if err := ValidateDSP(chain, plugins); err != nil {
		return nil, err
	}
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		return nil, err
	}
	for _, config := range plugins {
		if err := registry.Register(config.Name, func(params map[string]float64) (audio.Node, error) {
			if config.Builtin {
				// 官方增益直接使用原生工厂，支持协商声道与较大处理块。
				if versions != nil {
					versions[config.Name] = bundledplugin.GainVersion
				}
				return registry.Create("gain", params)
			}
			return &externalDSP{ctx: ctx, config: config, params: params, versions: versions}, nil
		}); err != nil {
			return nil, err
		}
	}
	specs := append([]audio.NodeSpec{{Name: "gain", Version: audio.NodeVersion, Params: map[string]float64{"gainDB": gain}}}, chain...)
	result, err := audio.NewChain(registry, specs)
	if err != nil {
		return nil, err
	}
	if err = result.Configure(format); err != nil {
		result.Close()
		return nil, err
	}
	if versions != nil {
		for _, spec := range chain {
			if short := builtin.EffectName(spec.Name); short != "" {
				versions["musicfs.dsp."+short] = builtin.Version(spec.Name)
			}
		}
	}
	return result, nil
}

func dspRevision(cfg Config, workerSHA string) string {
	chain := cfg.DSPChain
	if chain == nil {
		chain = []audio.NodeSpec{}
	}
	layout := cfg.OutputLayout
	if layout == "" {
		layout = "stereo"
	}
	versions := map[string]string{"gain": bundledplugin.GainVersion}
	for _, node := range cfg.DSPChain {
		if version := builtin.Version(node.Name); version != "" {
			versions[node.Name] = version
		}
	}
	data, _ := json.Marshal(struct {
		ProtocolVersion   int                `json:"protocolVersion"`
		WorkerSHA         string             `json:"workerSHA256"`
		WorkerVersion     string             `json:"workerVersion"`
		ChannelMixVersion string             `json:"channelMixVersion"`
		OutputLayout      string             `json:"outputLayout"`
		BufferFrames      int                `json:"bufferFrames"`
		BuiltinVersions   map[string]string  `json:"builtinVersions"`
		Chain             []audio.NodeSpec   `json:"chain"`
		Plugins           []dspworker.Config `json:"plugins"`
	}{dspworker.ProtocolVersion, workerSHA, audioPluginVersion, bundledplugin.ChannelMixVersion, layout, effectiveBufferFrames(cfg.BufferFrames), versions, chain, cfg.DSPPlugins})
	hash := sha256.Sum256(data)
	return "dsp-v1:" + hex.EncodeToString(hash[:])
}
