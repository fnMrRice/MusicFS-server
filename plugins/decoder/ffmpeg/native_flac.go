package audioffmpeg

import (
	"encoding/binary"
	"io"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// NativeFLAC 保存原生 FLAC 的 STREAMINFO 属性；码率仍由音频探测提供。
type NativeFLAC struct {
	Channels, SampleRate int
	DurationMs           *int64
}

// InspectNativeFLAC 校验固定快照的容器边界与首帧标识，不依赖扩展名或扫描标签。
// 这里只进行有界结构检查，完整帧解码和校验由播放器完成。
func InspectNativeFLAC(path string, fileSize int64) (NativeFLAC, error) {
	f, err := os.Open(path)
	if err != nil {
		return NativeFLAC{}, err
	}
	defer f.Close()
	invalid := func() error { return domain.Fail("UNSUPPORTED", "FLAC is not a verified native representation") }
	var header [42]byte
	if fileSize < int64(len(header))+6 {
		return NativeFLAC{}, invalid()
	}
	if _, err := io.ReadFull(f, header[:]); err != nil || string(header[:4]) != "fLaC" || header[4]&0x7f != 0 || flacBlockLength(header[5:8]) != 34 {
		return NativeFLAC{}, invalid()
	}
	info := header[8:]
	minimumBlock, maximumBlock := binary.BigEndian.Uint16(info[:2]), binary.BigEndian.Uint16(info[2:4])
	minimumFrame, maximumFrame := flacBlockLength(info[4:7]), flacBlockLength(info[7:10])
	properties := binary.BigEndian.Uint64(info[10:18])
	rate, channels, bits := int(properties>>44), int(properties>>41&7)+1, int(properties>>36&31)+1
	if minimumBlock < 16 || maximumBlock < minimumBlock || minimumFrame > 0 && maximumFrame > 0 && maximumFrame < minimumFrame || rate == 0 || bits < 4 {
		return NativeFLAC{}, invalid()
	}
	result := NativeFLAC{Channels: channels, SampleRate: rate}
	if samples := int64(properties & ((1 << 36) - 1)); samples > 0 {
		duration := samples * 1000 / int64(rate)
		result.DurationMs = &duration
	}
	offset, last := int64(len(header)), header[4]&0x80 != 0
	for blocks := 0; !last; blocks++ {
		var block [4]byte
		if blocks >= 1024 || offset > fileSize-4 {
			return NativeFLAC{}, invalid()
		}
		if _, err := f.ReadAt(block[:], offset); err != nil || block[0]&0x7f == 0 || block[0]&0x7f == 127 {
			return NativeFLAC{}, invalid()
		}
		length := int64(flacBlockLength(block[1:]))
		if length > fileSize-offset-4 {
			return NativeFLAC{}, invalid()
		}
		offset += 4 + length
		last = block[0]&0x80 != 0
	}
	var frame [4]byte
	if offset > fileSize-6 {
		return NativeFLAC{}, invalid()
	}
	if _, err := f.ReadAt(frame[:], offset); err != nil || frame[0] != 0xff || frame[1]&0xfe != 0xf8 || frame[2]>>4 == 0 || frame[2]&15 == 15 || frame[3]>>4 > 10 || frame[3]&1 != 0 || frame[3]>>1&7 == 3 {
		return NativeFLAC{}, invalid()
	}
	return result, nil
}

func flacBlockLength(data []byte) uint32 {
	return uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2])
}
