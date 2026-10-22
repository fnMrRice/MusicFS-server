package audioffmpeg

import (
	"encoding/binary"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

// FormatDetector 声明通用编解码插件支持的文件头；实际解码仍由已验证的 FFmpeg 执行。
func FormatDetector() audioformat.Detector {
	return audioformat.Detector{
		PluginID: "musicfs.plugin.decoder.ffmpeg", Priority: 10,
		Extensions: []string{".wav", ".mp3", ".flac", ".ogg", ".m4a", ".aac", ".aiff", ".aif", ".opus", ".webm"},
		Probe:      probeFormat,
	}
}

func probeFormat(header []byte, size int64) (audioformat.Match, bool) {
	var match audioformat.Match
	switch {
	case len(header) >= 12 && (string(header[:4]) == "RIFF" || string(header[:4]) == "RF64") && string(header[8:12]) == "WAVE":
		match = audioformat.Match{Format: "wav", MIME: "audio/wav", DurationMs: waveDuration(header, size)}
	case len(header) >= 4 && string(header[:4]) == "fLaC":
		match = audioformat.Match{Format: "flac", MIME: "audio/flac"}
	case len(header) >= 2 && header[0] == 0xff && header[1]&0xf6 == 0xf0:
		match = audioformat.Match{Format: "aac", MIME: "audio/aac"}
	case len(header) >= 3 && string(header[:3]) == "ID3", len(header) >= 2 && header[0] == 0xff && header[1]&0xe0 == 0xe0 && header[1]&0x18 != 0x08 && header[1]&0x06 != 0:
		match = audioformat.Match{Format: "mp3", MIME: "audio/mpeg"}
	case len(header) >= 4 && string(header[:4]) == "OggS":
		match = audioformat.Match{Format: "ogg", MIME: "audio/ogg"}
	case len(header) >= 12 && string(header[4:8]) == "ftyp" && audioMP4Brand(string(header[8:12])):
		match = audioformat.Match{Format: "mp4", MIME: "audio/mp4"}
	case len(header) >= 12 && string(header[:4]) == "FORM" && (string(header[8:12]) == "AIFF" || string(header[8:12]) == "AIFC"):
		match = audioformat.Match{Format: "aiff", MIME: "audio/aiff"}
	case webMHeader(header):
		match = audioformat.Match{Format: "webm", MIME: "audio/webm"}
	default:
		return audioformat.Match{}, false
	}
	return match, true
}

// WebM 必须由 EBML 头内的 DocType 证明，不能凭任意位置出现的字符串识别。
func webMHeader(header []byte) bool {
	if len(header) < 5 || string(header[:4]) != "\x1a\x45\xdf\xa3" {
		return false
	}
	length, width, ok := ebmlSize(header[4:])
	if !ok || length > uint64(len(header)-4-width) {
		return false
	}
	data := header[4+width : 4+width+int(length)]
	for len(data) > 0 {
		_, idWidth, ok := ebmlSize(data)
		if !ok || idWidth > 4 || len(data) == idWidth {
			return false
		}
		id := data[:idWidth]
		length, width, ok := ebmlSize(data[idWidth:])
		if !ok || length > uint64(len(data)-idWidth-width) {
			return false
		}
		value := data[idWidth+width : idWidth+width+int(length)]
		if string(id) == "\x42\x82" {
			return string(value) == "webm"
		}
		data = data[idWidth+width+int(length):]
	}
	return false
}

func ebmlSize(data []byte) (uint64, int, bool) {
	if len(data) == 0 || data[0] == 0 {
		return 0, 0, false
	}
	width, mask := 1, byte(0x80)
	for data[0]&mask == 0 {
		width++
		mask >>= 1
	}
	if len(data) < width {
		return 0, 0, false
	}
	value := uint64(data[0] & (mask - 1))
	for _, next := range data[1:width] {
		value = value<<8 | uint64(next)
	}
	return value, width, true
}

func waveDuration(header []byte, size int64) *int64 {
	if len(header) < 44 || string(header[12:16]) != "fmt " || binary.LittleEndian.Uint32(header[16:20]) != 16 || string(header[36:40]) != "data" {
		return nil
	}
	rate := binary.LittleEndian.Uint32(header[28:32])
	dataSize := binary.LittleEndian.Uint32(header[40:44])
	if rate == 0 || int64(dataSize) > size-44 {
		return nil
	}
	duration := int64(dataSize) * 1000 / int64(rate)
	return &duration
}

func audioMP4Brand(brand string) bool {
	switch brand {
	case "M4A ", "M4B ", "M4P ", "isom", "iso2", "mp41", "mp42", "qt  ", "dash":
		return true
	default:
		return false
	}
}
