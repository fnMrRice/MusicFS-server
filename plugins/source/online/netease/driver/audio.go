package driver

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/netease/quality"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

var mediaMD5 = regexp.MustCompile(`^[a-fA-F0-9]{32}$`)

type playerResource struct {
	ID            json.Number     `json:"id"`
	Code          int             `json:"code"`
	URL           string          `json:"url"`
	Level         string          `json:"level"`
	Type          string          `json:"type"`
	Encoding      string          `json:"encodeType"`
	MD5           string          `json:"md5"`
	Size          int64           `json:"size"`
	Bitrate       int64           `json:"br"`
	SampleRate    int             `json:"sr"`
	ChannelLayout json.RawMessage `json:"channelLayout"`
	Time          int64           `json:"time"`
	FreeTrial     json.RawMessage `json:"freeTrialInfo"`
}

// AudioResource 只使用当前已登录账号的播放接口。签名地址只在插件内部流转。
func (c *Client) AudioResource(ctx context.Context, id string) (pluginproto.OnlineAudio, string, error) {
	var empty pluginproto.OnlineAudio
	if !numericID.MatchString(id) {
		return empty, "", fault.Fail("INVALID_ARGUMENT", "Track identifier is invalid")
	}
	level := provider.PlaybackQuality(ctx)
	if level == "" {
		level = quality.Default
	}
	if !quality.Supported(level) {
		return empty, "", fault.Fail("INVALID_ARGUMENT", "Unknown NetEase playback quality")
	}
	query := url.Values{"ids": {"[" + id + "]"}, "level": {level}, "encodeType": {"flac"}}
	if level == "sky" {
		query.Set("immerseType", "aac")
	}
	var result struct {
		Code int              `json:"code"`
		Data []playerResource `json:"data"`
	}
	if err := c.WaitForMetadata(ctx); err != nil {
		return empty, "", err
	}
	if err := c.Get(ctx, "/api/song/enhance/player/url/v1", query, &result); err != nil {
		return empty, "", err
	}
	if err := c.BusinessError(result.Code, 200); err != nil {
		return empty, "", err
	}
	if len(result.Data) != 1 || result.Data[0].ID.String() != id {
		return empty, "", upstreamInvalid()
	}
	r := result.Data[0]
	if err := c.BusinessError(r.Code, 200); err != nil {
		return empty, "", err
	}
	if r.URL == "" || len(r.FreeTrial) != 0 && string(r.FreeTrial) != "null" {
		return empty, "", fault.Fail("ACCESS_DENIED", "The account cannot play the full NetEase track")
	}
	if r.Level != "higher" && !quality.Supported(r.Level) || r.Size <= 0 {
		return empty, "", upstreamInvalid()
	}
	audio := pluginproto.OnlineAudio{ExternalID: id, Quality: r.Level, SizeBytes: r.Size, SampleRateHz: r.SampleRate}
	// 仅映射明确的布局名称。该字段常为空，音质名称本身无法确定声道数。
	var layout string
	if json.Unmarshal(r.ChannelLayout, &layout) == nil {
		audio.Channels = map[string]int{"mono": 1, "stereo": 2, "5.1": 6, "5.1(side)": 6, "7.1": 8}[strings.ToLower(strings.TrimSpace(layout))]
	}
	if r.MD5 != "" {
		if !mediaMD5.MatchString(r.MD5) {
			return empty, "", upstreamInvalid()
		}
		audio.Version = strings.ToLower(r.MD5)
	}
	if r.Bitrate > 0 {
		audio.BitrateBps = &r.Bitrate
	}
	if r.Time > 0 {
		audio.DurationMs = &r.Time
	}
	switch strings.ToLower(r.Type) {
	case "mp3":
		audio.MIME, audio.Codec, audio.Container = "audio/mpeg", "mp3", "mp3"
	case "flac":
		audio.MIME, audio.Codec, audio.Container = "audio/flac", "flac", "flac"
	case "m4a", "mp4":
		if !strings.EqualFold(r.Encoding, "aac") {
			return empty, "", fault.Fail("UNSUPPORTED", "NetEase MP4 audio codec is not supported")
		}
		audio.MIME, audio.Codec, audio.Container = "audio/mp4", "aac", "m4a"
	default:
		return empty, "", fault.Fail("UNSUPPORTED", "NetEase audio format is not supported")
	}
	if err := audio.Validate(); err != nil {
		return empty, "", err
	}
	return audio, r.URL, nil
}
