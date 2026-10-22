// Package codecsettings 将编解码插件声明接入宿主的每用户设置存储。
package codecsettings

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

const FFmpegID = "musicfs.plugin.decoder.ffmpeg"
const NCMID = "musicfs.plugin.decoder.ncm"

func Providers() []settings.Provider {
	return []settings.Provider{{ID: FFmpegID, Schema: audioffmpeg.SettingsSchema()}, {ID: NCMID, Schema: ncmdecoder.SettingsSchema()}}
}

// DecodeOutput 读取已保存的格式；老存储实现仍使用插件默认值。
func DecodeOutput(ctx context.Context, repository any, owner string) (string, error) {
	repo, ok := repository.(settings.Repository)
	if !ok {
		return "native", nil
	}
	record, err := repo.GetPluginSettings(ctx, owner, FFmpegID)
	if err != nil {
		return "", err
	}
	values := record.Values
	if values == nil {
		values = settings.Defaults(audioffmpeg.SettingsSchema())
	}
	values, err = settings.Normalize(audioffmpeg.SettingsSchema(), values)
	if err != nil {
		return "", err
	}
	return values["decodeOutput"].(string), nil
}
