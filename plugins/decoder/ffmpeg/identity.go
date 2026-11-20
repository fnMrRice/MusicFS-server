package audioffmpeg

import "github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"

var audioPluginVersion = bundledplugin.FFmpegVersion

// ProcessingIdentity describes the verified, fixed startup toolchain. A zero
// Processor has no verified worker identity and cannot publish reusable output.
type ProcessingIdentity struct {
	WorkerSHA256, FFmpegSHA256, PluginVersion, PipelineRevision string
}

func (p *Processor) ProcessingIdentity() ProcessingIdentity {
	if p == nil {
		return ProcessingIdentity{}
	}
	return p.identity
}
