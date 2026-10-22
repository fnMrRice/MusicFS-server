// Package bundledplugin declares the release versions of the official bundled
// plugins. Workers and host metadata use these same declarations; each plugin
// release evolves independently of its wire protocol and the server version.
package bundledplugin

const (
	LocalFilesVersion   = "0.3.0"
	FFmpegVersion       = "0.5.0"
	NCMVersion          = "0.1.0"
	LocalScraperVersion = "0.1.0"
	NetEaseVersion      = "0.1.0"
	QQMusicVersion      = "0.1.0"
	AppleMusicVersion   = "0.1.0"
	GainVersion         = "0.1.0"
	EqualizerVersion    = "0.1.0"
	CompressorVersion   = "0.1.0"
	ReverbVersion       = "0.1.0"
	ChannelMixVersion   = "0.1.0"
	SQLiteVersion       = "0.1.0"
	ChromaprintVersion  = "0.1.0"
	FLACVersion         = "0.1.0"
	MatchingVersion     = "0.1.0"
	ThumbnailVersion    = "0.1.0"
)

// Version returns the package's declared release, including before a worker
// starts. Unknown plugins have no declaration and must supply their own version.
func Version(pluginID string) string {
	switch pluginID {
	case "musicfs.plugin.analysis.chromaprint":
		return ChromaprintVersion
	case "musicfs.plugin.tags.flac":
		return FLACVersion
	case "musicfs.plugin.matching.standard":
		return MatchingVersion
	case "musicfs.plugin.transform.thumbnail":
		return ThumbnailVersion
	case "musicfs.plugin.source.localfiles":
		return LocalFilesVersion
	case "musicfs.plugin.decoder.ffmpeg":
		return FFmpegVersion
	case "musicfs.plugin.decoder.ncm":
		return NCMVersion
	case "musicfs.scraper.local":
		return LocalScraperVersion
	case "musicfs.plugin.source.netease":
		return NetEaseVersion
	case "musicfs.plugin.source.qqmusic":
		return QQMusicVersion
	case "musicfs.plugin.source.applemusic":
		return AppleMusicVersion
	case "musicfs.dsp.gain":
		return GainVersion
	case "musicfs.dsp.equalizer":
		return EqualizerVersion
	case "musicfs.dsp.compressor":
		return CompressorVersion
	case "musicfs.dsp.reverb":
		return ReverbVersion
	case "musicfs.plugin.decoder.channelmix":
		return ChannelMixVersion
	case "musicfs.plugin.storage.sqlite":
		return SQLiteVersion
	default:
		return ""
	}
}
