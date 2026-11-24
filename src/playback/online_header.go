package playback

import (
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
)

// 探测只读取原始声道标志，跳过可含大封面的 ID3v2；损坏或不识别的头交由已配置的音频探测器处理。
func onlineHeaderChannels(r io.ReaderAt, codec string, size int64) int {
	if codec != "flac" && codec != "mp3" {
		return 0
	}
	offset, ok := afterID3(r, size)
	if !ok {
		return 0
	}
	if codec == "flac" {
		var h [42]byte
		if _, err := r.ReadAt(h[:], offset); err == nil {
			if _, ch, _, ok := audiotypes.FLACStreamInfo(h[:]); ok {
				return ch
			}
		}
		return 0
	}
	// 使用连续两帧的同步字、采样率及模式确认声道，避免把标签中的孤立字节误判为音频帧。
	var h, next [4]byte
	if _, err := r.ReadAt(h[:], offset); err != nil {
		return 0
	}
	frame, count, _ := mpegLayer3Frame(h)
	if frame == 0 || offset+int64(frame)+4 > size {
		return 0
	}
	if _, err := r.ReadAt(next[:], offset+int64(frame)); err != nil {
		return 0
	}
	nextSize, nextCount, _ := mpegLayer3Frame(next)
	if nextSize == 0 || count != nextCount || h[1]&0xfe != next[1]&0xfe || h[2]&12 != next[2]&12 {
		return 0
	}
	return count
}

func afterID3(r io.ReaderAt, size int64) (int64, bool) {
	var h [10]byte
	if _, err := r.ReadAt(h[:], 0); err != nil {
		return 0, false
	}
	if string(h[:3]) != "ID3" {
		return 0, true
	}
	if h[3] < 2 || h[3] > 4 || h[4] == 0xff || (h[6]|h[7]|h[8]|h[9])&0x80 != 0 {
		return 0, false
	}
	offset := 10 + int64(h[6])<<21 + int64(h[7])<<14 + int64(h[8])<<7 + int64(h[9])
	if h[3] == 4 && h[5]&0x10 != 0 {
		offset += 10
	}
	return offset, offset < size
}

func mpegLayer3Frame(h [4]byte) (length, channels, rate int) {
	version, index, sample := h[1]>>3&3, h[2]>>4, h[2]>>2&3
	if h[0] != 0xff || h[1]&0xe6 != 0xe2 || version == 1 || index == 0 || index == 15 || sample == 3 || h[3]&3 == 2 {
		return 0, 0, 0
	}
	rates := [...]int{44100, 48000, 32000}
	v1 := [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
	v2 := [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160}
	rate = rates[sample]
	length = 144000 * v1[index] / rate
	if version != 3 {
		rate /= 2
		if version == 0 {
			rate /= 2
		}
		length = 72000 * v2[index] / rate
	}
	channels = 2
	if h[3]>>6 == 3 {
		channels = 1
	}
	return length + int(h[2]>>1&1), channels, rate
}
