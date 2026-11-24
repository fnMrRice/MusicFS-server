package audioffmpeg

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type Capabilities struct {
	Version          string       `json:"version"`
	Build            string       `json:"build"`
	FFmpegSHA256     string       `json:"ffmpegSHA256"`
	FFprobeSHA256    string       `json:"ffprobeSHA256"`
	Profiles         []Profile    `json:"profiles"`
	PCM              audio.Format `json:"pcm"`
	Normalization    bool         `json:"normalization"`
	DecodeOutputs    []string     `json:"decodeOutputs,omitempty"`
	TranscodeFormats []string     `json:"transcodeFormats,omitempty"`
}
type Profile = audiotypes.Profile
type cappedBuffer struct {
	b   []byte
	max int
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remaining := b.max - len(b.b)
	if remaining > 0 {
		b.b = append(b.b, p[:min(remaining, len(p))]...)
	}
	return n, nil
}

func executableHash(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("UNSUPPORTED: tool path must be absolute")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("UNSUPPORTED: required executable is unavailable")
	}
	defer f.Close()
	i, err := f.Stat()
	if err != nil || !i.Mode().IsRegular() {
		return "", fmt.Errorf("UNSUPPORTED: invalid executable")
	}
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

var errProbeOutputLimit = errors.New("UNSUPPORTED: tool capability output exceeds limit")

func toolOutput(ctx context.Context, path string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	out := &cappedBuffer{max: 1 << 20}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("UNSUPPORTED: tool probe failed (%v): %s", err, redactToolOutput(string(out.b)))
	}
	if len(out.b) >= out.max {
		return "", errProbeOutputLimit
	}
	return string(out.b), nil
}

func redactToolOutput(s string) string {
	s = regexp.MustCompile(`https?://[^\s]+`).ReplaceAllString(s, "[task-channel]")
	if len(s) > 2048 {
		s = s[:2048]
	}
	return s
}

func Probe(ctx context.Context, ffmpeg, ffprobe string) (Capabilities, error) {
	var c Capabilities
	var err error
	c.FFmpegSHA256, err = executableHash(ffmpeg)
	if err != nil {
		return c, err
	}
	c.FFprobeSHA256, err = executableHash(ffprobe)
	if err != nil {
		return c, err
	}
	version, err := toolOutput(ctx, ffmpeg, "-version")
	if err != nil {
		return c, err
	}
	if !strings.HasPrefix(version, "ffmpeg version ") {
		return c, fmt.Errorf("UNSUPPORTED: invalid FFmpeg version output")
	}
	// Windows 工具输出使用 CRLF；版本身份不能携带行尾控制字符。
	c.Version = strings.TrimSpace(strings.Split(version, "\n")[0])
	c.Build = version
	probe, err := toolOutput(ctx, ffprobe, "-version")
	if err != nil || !strings.HasPrefix(probe, "ffprobe version ") {
		return c, fmt.Errorf("UNSUPPORTED: invalid ffprobe version")
	}
	encoders, err := toolOutput(ctx, ffmpeg, "-hide_banner", "-encoders")
	if err != nil {
		return c, err
	}
	decoders, err := toolOutput(ctx, ffmpeg, "-hide_banner", "-decoders")
	if err != nil {
		return c, err
	}
	demuxers, err := toolOutput(ctx, ffmpeg, "-hide_banner", "-demuxers")
	if err != nil {
		return c, err
	}
	muxers, err := toolOutput(ctx, ffmpeg, "-hide_banner", "-muxers")
	if err != nil {
		return c, err
	}
	c.Profiles, err = profilesForComponents(encoders, decoders, demuxers, muxers)
	if err != nil {
		return c, err
	}
	c.PCM = audio.Canonical
	c.DecodeOutputs = []string{DecodeFloat32}
	c.TranscodeFormats = []string{"mp3"}
	if strings.Contains(encoders, "pcm_s16le") {
		c.DecodeOutputs = append(c.DecodeOutputs, DecodeSigned16)
		c.TranscodeFormats = append(c.TranscodeFormats, "wav")
	}
	if strings.Contains(encoders, "flac") && strings.Contains(muxers, " flac ") {
		c.TranscodeFormats = append(c.TranscodeFormats, "flac")
	}
	if strings.Contains(encoders, "libopus") && strings.Contains(muxers, " webm ") {
		c.TranscodeFormats = append(c.TranscodeFormats, "opus")
	}
	c.Normalization = strings.Contains(encoders, "pcm_s16le") && strings.Contains(muxers, " s16le ")
	return c, nil
}

func profilesForComponents(encoders, decoders, demuxers, muxers string) ([]Profile, error) {
	if !strings.Contains(encoders, "libmp3lame") || !strings.Contains(encoders, "pcm_f32le") || !strings.Contains(decoders, "pcm_f32le") || !strings.Contains(demuxers, " wav ") || !strings.Contains(demuxers, " f32le ") || !strings.Contains(muxers, " mp3 ") || !strings.Contains(muxers, " wav ") {
		return nil, fmt.Errorf("UNSUPPORTED: required audio components unavailable")
	}
	stereo, _ := ProfileByID("mp3-128")
	profiles := []Profile{stereo}
	if strings.Contains(encoders, "libopus") && strings.Contains(muxers, " webm ") {
		for _, id := range []string{"opus-5.1", "opus-7.1"} {
			profile, _ := ProfileByID(id)
			profiles = append(profiles, profile)
		}
	}
	return profiles, nil
}
