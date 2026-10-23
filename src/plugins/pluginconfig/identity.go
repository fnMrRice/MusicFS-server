package pluginconfig

import "github.com/fnMrRice/MusicFS-server/sdk/pluginproto"

// Identity 返回可信注册声明；未接入通用实例的官方基础设施使用默认显示信息。
func Identity(pluginID string) *pluginproto.PluginIdentity {
	if d, ok := Lookup(pluginID); ok {
		return d.Identity
	}
	return builtinIdentity(pluginID)
}

func builtinIdentity(pluginID string) *pluginproto.PluginIdentity {
	identity := &pluginproto.PluginIdentity{ID: pluginID, LocalizedNames: map[string]string{}, Features: []string{}}
	switch pluginID {
	case "musicfs.plugin.analysis.chromaprint":
		identity.Name = "Chromaprint Audio Analysis"
		identity.LocalizedNames = map[string]string{"zh": "Chromaprint 音频分析", "zh-Hans": "Chromaprint 音频分析", "zh-Hant": "Chromaprint 音訊分析"}
		identity.Features = []string{"audio-analysis"}
	case "musicfs.plugin.tags.flac":
		identity.Name = "FLAC Tag Format"
		identity.LocalizedNames = map[string]string{"zh": "FLAC 标签格式", "zh-Hans": "FLAC 标签格式", "zh-Hant": "FLAC 標籤格式"}
		identity.Features = []string{"metadata"}
	case "musicfs.plugin.matching.standard":
		identity.Name = "Standard Recording Matching"
		identity.LocalizedNames = map[string]string{"zh": "标准录音匹配", "zh-Hans": "标准录音匹配", "zh-Hant": "標準錄音匹配"}
		identity.Features = []string{"matching"}
	case "musicfs.plugin.transform.thumbnail":
		identity.Name = "Artwork Thumbnail"
		identity.LocalizedNames = map[string]string{"zh": "封面缩略图", "zh-Hans": "封面缩略图", "zh-Hant": "封面縮圖"}
		identity.Features = []string{"metadata"}
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
	case "musicfs.plugin.source.bilibili":
		identity.Name = "Bilibili"
		identity.LocalizedNames = map[string]string{"zh": "哔哩哔哩", "zh-Hans": "哔哩哔哩", "zh-Hant": "嗶哩嗶哩"}
		identity.Features = []string{"music-source"}
	case "musicfs.plugin.decoder.ffmpeg":
		identity.Name = "FFmpeg Audio Codec"
		identity.LocalizedNames = map[string]string{"zh": "FFmpeg 音频编解码", "zh-Hans": "FFmpeg 音频编解码", "zh-Hant": "FFmpeg 音訊編解碼"}
		identity.Features = []string{"audio-codec"}
	case "musicfs.plugin.decoder.channelmix":
		identity.Name = "Audio Channel Conversion"
		identity.LocalizedNames = map[string]string{"zh": "音频声道转换", "zh-Hans": "音频声道转换", "zh-Hant": "音訊聲道轉換"}
		identity.Features = []string{"audio-dsp"}
	case "musicfs.plugin.decoder.ncm":
		identity.Name = "NCM Decryption"
		identity.LocalizedNames = map[string]string{"zh": "NCM 解密", "zh-Hans": "NCM 解密", "zh-Hant": "NCM 解密"}
		identity.Features = []string{"audio-codec"}
	case "musicfs.scraper.local":
		identity.Name = "Local Metadata Extraction"
		identity.LocalizedNames = map[string]string{"zh": "本地元数据提取", "zh-Hans": "本地元数据提取", "zh-Hant": "本機中繼資料擷取"}
		identity.Features = []string{"metadata"}
	case "musicfs.plugin.storage.sqlite":
		identity.Name = "SQLite Database"
		identity.LocalizedNames = map[string]string{"zh": "SQLite 数据库", "zh-Hans": "SQLite 数据库", "zh-Hant": "SQLite 資料庫"}
		identity.Features = []string{"database"}
	case "musicfs.dsp.gain":
		identity.Name = "DSP Gain"
		identity.LocalizedNames = map[string]string{"zh": "DSP 增益", "zh-Hans": "DSP 增益", "zh-Hant": "DSP 增益"}
		identity.Features = []string{"audio-dsp"}
	case "musicfs.dsp.equalizer":
		identity.Name = "DSP Equalizer"
		identity.LocalizedNames = map[string]string{"zh": "DSP 均衡器", "zh-Hans": "DSP 均衡器", "zh-Hant": "DSP 等化器"}
		identity.Features = []string{"audio-dsp"}
	case "musicfs.dsp.compressor":
		identity.Name = "DSP Compressor"
		identity.LocalizedNames = map[string]string{"zh": "DSP 压缩器", "zh-Hans": "DSP 压缩器", "zh-Hant": "DSP 壓縮器"}
		identity.Features = []string{"audio-dsp"}
	case "musicfs.dsp.reverb":
		identity.Name = "DSP Reverb"
		identity.LocalizedNames = map[string]string{"zh": "DSP 混响", "zh-Hans": "DSP 混响", "zh-Hant": "DSP 殘響"}
		identity.Features = []string{"audio-dsp"}
	default:
		return nil
	}
	return identity
}
