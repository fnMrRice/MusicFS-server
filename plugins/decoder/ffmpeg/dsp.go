package audioffmpeg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspchain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

// ValidateDSP checks configuration without opening files or starting workers.
func ValidateDSP(chain []audio.NodeSpec, plugins []dspworker.Config) error {
	return dspchain.Validate(chain, plugins)
}

func CloneDSPChain(chain []audio.NodeSpec) []audio.NodeSpec {
	return dspchain.Clone(chain)
}

func (p *Processor) HasDSP() bool { return p != nil && len(p.config.DSPChain) != 0 }

// DSPPluginVersion returns a release verified during processor initialization.
// Reading it does not start an idle worker or query a playback process.
func (p *Processor) DSPPluginVersion(name string) string {
	if p == nil {
		return ""
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
	if err := ValidateDSP(chain, plugins); err != nil {
		return nil, err
	}
	registry := audio.NewRegistry()
	if err := dsp.Register(registry); err != nil {
		return nil, err
	}
	for _, config := range plugins {
		if err := registry.Register(config.Name, func(params map[string]float64) (audio.Node, error) {
			process, err := dspworker.New(ctx, config, params)
			if err == nil && versions != nil {
				versions[config.Name] = process.PluginVersion()
			}
			return process, err
		}); err != nil {
			return nil, err
		}
	}
	specs := append([]audio.NodeSpec{{Name: "gain", Version: audio.NodeVersion, Params: map[string]float64{"gainDB": gain}}}, chain...)
	result, err := audio.NewChain(registry, specs)
	if err != nil {
		return nil, err
	}
	if err = result.Configure(audio.Canonical); err != nil {
		result.Close()
		return nil, err
	}
	return result, nil
}

func dspRevision(cfg Config, workerSHA string) string {
	if len(cfg.DSPChain) == 0 {
		return "gain-v1"
	}
	data, _ := json.Marshal(struct {
		ProtocolVersion int                `json:"protocolVersion"`
		WorkerSHA       string             `json:"workerSHA256"`
		WorkerVersion   string             `json:"workerVersion"`
		Chain           []audio.NodeSpec   `json:"chain"`
		Plugins         []dspworker.Config `json:"plugins"`
	}{dspworker.ProtocolVersion, workerSHA, audioPluginVersion, cfg.DSPChain, cfg.DSPPlugins})
	hash := sha256.Sum256(data)
	return "dsp-v1:" + hex.EncodeToString(hash[:])
}
