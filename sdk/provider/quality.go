package provider

import "context"

type playbackQualityKey struct{}

// WithPlaybackQuality 将用户在本次播放选择的音质随描述和打开请求传给插件。
func WithPlaybackQuality(ctx context.Context, quality string) context.Context {
	return context.WithValue(ctx, playbackQualityKey{}, quality)
}

// PlaybackQuality 为空时由插件采用其默认播放音质。
func PlaybackQuality(ctx context.Context) string {
	quality, _ := ctx.Value(playbackQualityKey{}).(string)
	return quality
}
