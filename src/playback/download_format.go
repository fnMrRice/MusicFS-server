package playback

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
)

// 从已写满的本地文件读取属性，不使用来源给出的扩展名或音质标签来判断实际编码。
func (s *Service) probeDownloadedAudio(ctx context.Context, f *os.File, size int64) (audiotypes.Metadata, error) {
	offset, ok := afterID3(f, size)
	if !ok {
		return audiotypes.Metadata{}, errors.New("invalid audio header")
	}
	var header [42]byte
	n, _ := f.ReadAt(header[:], offset)
	rate, channels, samples, flac := audiotypes.FLACStreamInfo(header[:n])
	if flac {
		hz := int64(rate)
		format := "flac"
		md := audiotypes.Metadata{Codec: &format, Container: &format, MIME: "audio/flac", SampleRateHz: &hz,
			Channels: channels, BitDepth: int(binary.BigEndian.Uint64(header[18:26])>>36&31) + 1}
		if samples > 0 {
			bitrate := (size*8*hz + samples/2) / samples
			md.BitrateBps = &bitrate
		}
		return md, nil
	}
	if offset == 0 && n >= 12 && string(header[:4]) == "RIFF" && string(header[8:12]) == "WAVE" {
		if md, err := audiocodecs.InspectNative("audio/wav", f.Name(), size); err == nil {
			md.MIME, md.BitDepth = "audio/wav", 16 // 原生 WAV 检查器仅接受经过验证的 PCM16。
			return md, nil
		}
	}
	processor, _ := s.audioSnapshot()
	md, err := processor.ProbeOutputMetadata(ctx, io.NewSectionReader(f, 0, size), size)
	if err == nil {
		if md.MIME == "audio/mp4" {
			// ffprobe 返回 MOV/MP4 整个格式族；在线音频使用 M4A 容器名。
			container := "m4a"
			md.Container = &container
		}
		return md, nil
	}
	if ctx.Err() != nil {
		return audiotypes.Metadata{}, ctx.Err()
	}
	if n >= 4 && onlineHeaderChannels(f, "mp3", size) != 0 {
		return downloadedMP3(ctx, io.NewSectionReader(f, offset, size-offset))
	}
	return audiotypes.Metadata{}, err
}

// 无外部探测器时按完整 MPEG 音频帧计算平均码率，兼容 VBR，不把 ID3 标签算入音频。
func downloadedMP3(ctx context.Context, media io.Reader) (audiotypes.Metadata, error) {
	input := bufio.NewReaderSize(media, 64<<10)
	var byteCount, sampleCount int64
	var version byte
	var channels, rate int
	for ctx.Err() == nil {
		next, err := input.Peek(4)
		if err != nil {
			break
		}
		h := [4]byte(next)
		length, ch, hz := mpegLayer3Frame(h)
		if length == 0 {
			break
		}
		if sampleCount > 0 && (ch != channels || hz != rate || h[1]&0x18 != version) {
			return audiotypes.Metadata{}, errors.New("inconsistent MPEG audio frames")
		}
		if _, err := input.Discard(length); err != nil {
			return audiotypes.Metadata{}, io.ErrUnexpectedEOF
		}
		rate, channels, version = hz, ch, h[1]&0x18
		byteCount += int64(length)
		if version == 0x18 {
			sampleCount += 1152
		} else {
			sampleCount += 576
		}
	}
	if ctx.Err() != nil {
		return audiotypes.Metadata{}, ctx.Err()
	}
	if sampleCount == 0 {
		return audiotypes.Metadata{}, errors.New("empty MPEG audio")
	}
	hz := int64(rate)
	bitrate := (byteCount*8*hz + sampleCount/2) / sampleCount
	format := "mp3"
	return audiotypes.Metadata{MIME: "audio/mpeg", Codec: &format, Container: &format,
		SampleRateHz: &hz, Channels: channels, BitrateBps: &bitrate}, nil
}
