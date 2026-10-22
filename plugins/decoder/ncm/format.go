package ncmdecoder

import "github.com/fnMrRice/MusicFS-server/sdk/audioformat"

// FormatDetector 由 NCM 插件识别加密容器；宿主无需了解文件头或扩展名。
func FormatDetector() audioformat.Detector {
	return audioformat.Detector{
		PluginID: "musicfs.plugin.decoder.ncm", Priority: 100, Extensions: []string{".ncm"},
		Probe: func(header []byte, _ int64) (audioformat.Match, bool) {
			if len(header) < 8 || string(header[:8]) != "CTENFDAM" {
				return audioformat.Match{}, false
			}
			return audioformat.Match{Format: "ncm", MIME: "audio/x-ncm", RequiresDecode: true}, true
		},
	}
}
