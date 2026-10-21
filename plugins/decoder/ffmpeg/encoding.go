package audioffmpeg

import "strconv"

// encoderArguments 仅使用已注册的固定 profile，不接受外部滤镜或编码参数。
func encoderArguments(profile Profile) []string {
	args := []string{"-hide_banner", "-nostdin", "-v", "error", "-f", "f32le", "-ar", strconv.Itoa(profile.SampleRate), "-ac", strconv.Itoa(profile.Channels), "-channel_layout", profile.Layout, "-i", "pipe:0", "-vn"}
	if profile.Codec == "opus" {
		// FFmpeg 按输入的 WAVE 声道布局转换至 Opus mapping family 1 的顺序。
		return append(args, "-codec:a", "libopus", "-b:a", strconv.Itoa(profile.BitrateBps), "-mapping_family", "1", "-f", "webm", "pipe:1")
	}
	return append(args, "-codec:a", "libmp3lame", "-b:a", strconv.Itoa(profile.BitrateBps), "-f", "mp3", "-write_xing", "0", "-id3v2_version", "0", "pipe:1")
}
