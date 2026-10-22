package audioffmpeg

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// InspectNativeWave 验证可直接播放的 PCM16 表示，不依赖文件扩展名。
type NativeWave struct {
	Codec, Container       string
	DurationMs, BitrateBps int64
	Channels, SampleRate   int
}

func InspectNativeWave(path string, fileSize int64) (NativeWave, error) {
	f, err := os.Open(path)
	if err != nil {
		return NativeWave{}, err
	}
	defer f.Close()
	invalid := func() error { return domain.Fail("UNSUPPORTED", "WAV is not a verified PCM16 representation") }
	var header [12]byte
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return NativeWave{}, invalid()
	}
	end := int64(binary.LittleEndian.Uint32(header[4:8])) + 8
	if end > fileSize || end < 44 {
		return NativeWave{}, invalid()
	}
	var rate, block uint32
	var channels uint16
	var dataSize int64
	for offset, chunks := int64(12), 0; offset+8 <= end && chunks < 1024; chunks++ {
		var chunk [8]byte
		if _, err := f.ReadAt(chunk[:], offset); err != nil {
			return NativeWave{}, invalid()
		}
		size := int64(binary.LittleEndian.Uint32(chunk[4:]))
		if size > end-offset-8 {
			return NativeWave{}, invalid()
		}
		switch string(chunk[:4]) {
		case "fmt ":
			var format [16]byte
			if size < 16 || rate != 0 {
				return NativeWave{}, invalid()
			}
			if _, err := f.ReadAt(format[:], offset+8); err != nil {
				return NativeWave{}, invalid()
			}
			channels = binary.LittleEndian.Uint16(format[2:4])
			rate = binary.LittleEndian.Uint32(format[4:8])
			block = uint32(binary.LittleEndian.Uint16(format[12:14]))
			if binary.LittleEndian.Uint16(format[:2]) != 1 || (channels != 1 && channels != 2) || binary.LittleEndian.Uint16(format[14:]) != 16 || block != uint32(channels)*2 || rate < 8000 || rate > 192000 || binary.LittleEndian.Uint32(format[8:12]) != rate*block {
				return NativeWave{}, invalid()
			}
		case "data":
			if dataSize != 0 {
				return NativeWave{}, invalid()
			}
			dataSize = size
		}
		offset += 8 + size + size%2
	}
	if rate == 0 || dataSize <= 0 || dataSize%int64(block) != 0 {
		return NativeWave{}, invalid()
	}
	codec, container := "pcm_s16le", "wav"
	duration := dataSize * 1000 / int64(rate*block)
	bitrate := int64(rate*block) * 8
	return NativeWave{Codec: codec, Container: container, DurationMs: duration, BitrateBps: bitrate, Channels: int(channels), SampleRate: int(rate)}, nil
}
