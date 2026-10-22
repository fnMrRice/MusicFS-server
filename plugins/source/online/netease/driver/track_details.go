package driver

import (
	"context"
	"fmt"
	"strings"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

// TrackDetails 仅选择平台公开的歌曲元数据，字段及其顺序由网易来源定义。
func (c *Client) TrackDetails(ctx context.Context, externalID string) (pluginproto.TrackDetails, error) {
	song, err := c.metadataSong(ctx, externalID)
	if err != nil {
		return pluginproto.TrackDetails{}, err
	}
	track, err := song.track(true)
	if err != nil {
		return pluginproto.TrackDetails{}, err
	}
	fields := []pluginproto.TrackDetailField{{ID: "title", Label: "歌曲名", Value: track.Title}}
	appendText := func(id, label, value string) {
		if value != "" {
			fields = append(fields, pluginproto.TrackDetailField{ID: id, Label: label, Value: value})
		}
	}
	appendText("artists", "作者 / 艺人", strings.Join(track.Artists, " / "))
	appendText("album", "专辑", track.Album)
	if track.DurationMs != nil {
		ms := *track.DurationMs
		appendText("duration", "时长", fmt.Sprintf("%d:%02d.%03d", ms/60000, ms/1000%60, ms%1000))
	}
	for _, item := range []struct {
		id, label string
		values    []string
	}{
		{"aliases", "别名", song.Aliases},
		{"translations", "译名", song.Translations},
	} {
		value, err := detailNames(item.values)
		if err != nil {
			return pluginproto.TrackDetails{}, err
		}
		appendText(item.id, item.label, value)
	}
	result := pluginproto.TrackDetails{
		ExternalID: externalID, Title: track.Title,
		Sections: []pluginproto.TrackDetailSection{
			{ID: "track", Title: "歌曲信息", Fields: fields},
			{ID: "source", Title: "来源信息", Fields: []pluginproto.TrackDetailField{
				{ID: "platform", Label: "音乐平台", Value: "网易云音乐"},
				{ID: "externalId", Label: "平台歌曲 ID", Value: track.ExternalID},
				{ID: "sourceUrl", Label: "公开歌曲页面", Value: track.SourceURL},
			}},
		},
	}
	if err := result.Validate(externalID); err != nil {
		return pluginproto.TrackDetails{}, upstreamInvalid()
	}
	return result, nil
}

func detailNames(values []string) (string, error) {
	if len(values) > 128 {
		return "", upstreamInvalid()
	}
	names := make([]string, 0, len(values))
	for _, value := range values {
		clean, err := textValue(value, 4096)
		if err != nil {
			return "", err
		}
		if clean != "" {
			names = append(names, clean)
		}
	}
	return strings.Join(names, " / "), nil
}
