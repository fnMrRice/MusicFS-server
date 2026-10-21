package pluginconfig

import "github.com/fnMrRice/MusicFS-server/sdk/pluginproto"

// Identity owns the official plugin display declarations. Workers and hosts
// share these defaults; a verified worker may supply updated display names.
func Identity(pluginID string) *pluginproto.PluginIdentity {
	identity := &pluginproto.PluginIdentity{ID: pluginID, LocalizedNames: map[string]string{}, Features: []string{}}
	switch pluginID {
	case "musicfs.plugin.source.localfiles":
		identity.Name = "Local Music"
		identity.LocalizedNames = map[string]string{"zh": "本地音乐", "zh-Hans": "本地音乐", "zh-Hant": "本機音樂", "ja": "ローカル音楽"}
		identity.Features = []string{"music-source"}
	case "musicfs.plugin.source.netease":
		identity.Name = "NetEase Cloud Music"
		identity.LocalizedNames = map[string]string{"zh": "网易云音乐", "zh-Hans": "网易云音乐", "zh-Hant": "網易雲音樂"}
		identity.Features = []string{"music-source"}
	case "musicfs.plugin.source.qqmusic":
		identity.Name = "QQ Music"
		identity.LocalizedNames = map[string]string{"zh": "QQ 音乐", "zh-Hans": "QQ 音乐", "zh-Hant": "QQ 音樂"}
		identity.Features = []string{"music-source"}
	case "musicfs.plugin.source.applemusic":
		identity.Name = "Apple Music"
		identity.Features = []string{"music-source"}
	case "musicfs.plugin.decoder.ffmpeg":
		identity.Name = "FFmpeg Audio Processing"
		identity.LocalizedNames = map[string]string{"zh": "FFmpeg 音频处理", "zh-Hans": "FFmpeg 音频处理", "zh-Hant": "FFmpeg 音訊處理"}
		identity.Features = []string{"audio-processing"}
	case "musicfs.plugin.decoder.ncm":
		identity.Name = "NCM Decryption"
		identity.LocalizedNames = map[string]string{"zh": "NCM 解密", "zh-Hans": "NCM 解密", "zh-Hant": "NCM 解密"}
		identity.Features = []string{"audio-decryption"}
	case "musicfs.scraper.local":
		identity.Name = "Local Metadata Extraction"
		identity.LocalizedNames = map[string]string{"zh": "本地元数据提取", "zh-Hans": "本地元数据提取", "zh-Hant": "本機中繼資料擷取"}
		identity.Features = []string{"lyrics", "artwork"}
	case "musicfs.plugin.storage.sqlite":
		identity.Name = "SQLite Database"
		identity.LocalizedNames = map[string]string{"zh": "SQLite 数据库", "zh-Hans": "SQLite 数据库", "zh-Hant": "SQLite 資料庫"}
		identity.Features = []string{"database"}
	case "musicfs.dsp.gain":
		identity.Name = "DSP Gain"
		identity.LocalizedNames = map[string]string{"zh": "DSP 增益", "zh-Hans": "DSP 增益", "zh-Hant": "DSP 增益"}
		identity.Features = []string{"audio-dsp"}
	default:
		return nil
	}
	return identity
}
