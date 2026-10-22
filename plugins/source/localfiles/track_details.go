package localfiles

import (
	"context"
	"strconv"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func (w *worker) trackDetails(ctx context.Context, r pluginproto.ResolveRequest) (pluginproto.TrackDetails, error) {
	value, err := w.resolve(ctx, r, true)
	if err != nil {
		return pluginproto.TrackDetails{}, err
	}
	item := value.(pluginproto.Observation)
	fields := []pluginproto.TrackDetailField{
		{ID: "filename", Label: "文件名", Value: item.DisplayName},
		{ID: "relativePath", Label: "来源内路径", Value: item.RelativePath},
		{ID: "root", Label: "来源目录标识", Value: item.RootID},
		{ID: "size", Label: "文件大小", Value: strconv.FormatInt(item.SizeBytes, 10) + " 字节"},
		{ID: "mime", Label: "媒体类型", Value: item.MIME},
	}
	add := func(id, label string, value *string) {
		if value != nil && *value != "" {
			fields = append(fields, pluginproto.TrackDetailField{ID: id, Label: label, Value: *value})
		}
	}
	add("codec", "编码", item.Codec)
	add("bitrateMode", "码率模式", item.BitrateMode)
	if item.DurationMs != nil {
		fields = append(fields, pluginproto.TrackDetailField{ID: "duration", Label: "时长", Value: strconv.FormatInt(*item.DurationMs, 10) + " 毫秒"})
	}
	if item.BitrateBps != nil {
		fields = append(fields, pluginproto.TrackDetailField{ID: "bitrate", Label: "码率", Value: strconv.FormatInt(*item.BitrateBps, 10) + " bit/s"})
	}
	add("providerSha512", "来源声明 SHA-512", item.ProviderSHA512)
	return pluginproto.TrackDetails{ExternalID: item.ExternalID, Title: item.DisplayName, Sections: []pluginproto.TrackDetailSection{
		{ID: "file", Title: "文件信息", Fields: fields},
		{ID: "identity", Title: "来源标识", Fields: []pluginproto.TrackDetailField{
			{ID: "externalId", Label: "来源曲目标识", Value: item.ExternalID},
			{ID: "observedVersion", Label: "扫描版本", Value: item.ObservedVersion},
		}},
	}}, nil
}
