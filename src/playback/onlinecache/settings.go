// Package onlinecache 管理在线来源经过授权读取后的完整音频副本。
package onlinecache

import (
	"context"
	"math"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginsettings"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

const defaultLimitMiB = 1024

func RetentionDisabled() error {
	return domain.Fail("AUDIO_CACHE_DISABLED", "Audio cache retention is disabled for this source")
}

type Preferences struct {
	LimitBytes int64
	Stream     bool
	Quality    string
}

func SettingsFields() []pluginsettings.Field {
	minimum, maximum := 0.0, 1048576.0
	return []pluginsettings.Field{
		{
			Key: "cacheLimitMiB", Type: "number", Label: "Audio cache limit (MiB)",
			LocalizedLabels:       map[string]string{"zh": "音频缓存上限（MiB）", "zh-Hant": "音訊快取上限（MiB）", "ja": "音声キャッシュ上限（MiB）"},
			Description:           "Trim the oldest downloads daily. Zero disables retention.",
			LocalizedDescriptions: map[string]string{"zh": "每天优先清理最早下载的版本，直至低于上限。设为 0 时停止保留缓存。", "zh-Hant": "每天優先清理最早下載的版本，直到符合上限。設為 0 時停止保留快取。", "ja": "毎日、上限以内になるまで古いダウンロードから削除します。0 に設定すると保存しません。"},
			DefaultValue:          float64(defaultLimitMiB), Min: &minimum, Max: &maximum,
		},
		{
			Key: "streamWhileDownloading", Type: "checkbox", Label: "Play while downloading",
			LocalizedLabels:       map[string]string{"zh": "边下边播", "zh-Hant": "邊下載邊播放", "ja": "ダウンロードしながら再生"},
			Description:           "Start playback before the complete audio file arrives.",
			LocalizedDescriptions: map[string]string{"zh": "开启后收到音频即可播放，关闭后等待完整音频下载。", "zh-Hant": "開啟後收到音訊即可播放，關閉後等待完整音訊下載。", "ja": "有効にすると、ダウンロードが完了する前に再生を開始します。"},
			DefaultValue:          true,
		},
	}
}

func ValidateSettings(values settings.Values) error {
	if n, ok := values["cacheLimitMiB"]; ok {
		v, valid := n.(float64)
		if !valid || v < 0 || v > 1048576 || v != math.Trunc(v) {
			return domain.Fail("INVALID_ARGUMENT", "Audio cache limit must be a whole number of MiB")
		}
	}
	return nil
}

func ReadPreferences(ctx context.Context, repo settings.Repository, owner, plugin string) (Preferences, error) {
	p := Preferences{LimitBytes: defaultLimitMiB << 20, Stream: true}
	if repo == nil || plugin == "" {
		return p, nil
	}
	r, err := repo.GetPluginSettings(ctx, owner, plugin)
	if err != nil {
		return p, err
	}
	if err = ValidateSettings(r.Values); err != nil {
		return p, err
	}
	if n, ok := r.Values["cacheLimitMiB"].(float64); ok {
		p.LimitBytes = int64(n) << 20
	}
	if v, ok := r.Values["streamWhileDownloading"].(bool); ok {
		p.Stream = v
	}
	p.Quality, _ = r.Values["playbackQuality"].(string)
	return p, nil
}
