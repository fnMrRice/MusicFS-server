package audioffmpeg

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

const maxWAVHeaderBytes = 64 << 10

// readDecodedWAV 保留旧的 float32 默认值，不猜测未知多声道布局。
func readDecodedWAV(input io.Reader) (audio.Format, io.Reader, error) {
	return readDecodedWAVOutput(input, DecodeFloat32)
}

func readDecodedWAVOutput(input io.Reader, output string) (audio.Format, io.Reader, error) {
	output, err := DecodeOutput(output)
	if err != nil {
		return audio.Format{}, nil, err
	}
	sampleBytes := 4
	if output == DecodeSigned16 {
		sampleBytes = 2
	}
	var header [12]byte
	if _, err := io.ReadFull(input, header[:]); err != nil || string(header[:4]) != "RIFF" || string(header[8:]) != "WAVE" {
		return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid decoded WAV header")
	}
	riffSize := binary.LittleEndian.Uint32(header[4:])
	if riffSize != mathMaxUint32 {
		if riffSize < 4 {
			return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid decoded WAV size")
		}
		input = &exactWAVReader{input: input, remaining: int64(riffSize) - 4}
	}
	var format audio.Format
	headerBytes := 12
	for chunks := 0; chunks < 64; chunks++ {
		var chunk [8]byte
		if _, err := io.ReadFull(input, chunk[:]); err != nil {
			return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: incomplete decoded WAV header")
		}
		headerBytes += 8
		size := binary.LittleEndian.Uint32(chunk[4:])
		if string(chunk[:4]) == "data" {
			if format.Channels == 0 || size != mathMaxUint32 && size%uint32(format.Channels*sampleBytes) != 0 {
				return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid decoded WAV data")
			}
			if size == mathMaxUint32 {
				if riffSize != mathMaxUint32 {
					return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid streaming WAV size")
				}
				return format, decodedPCMReader(input, output), nil
			}
			return format, decodedPCMReader(&exactWAVReader{input: input, remaining: int64(size)}, output), nil
		}
		if size > maxWAVHeaderBytes || headerBytes+int(size)+int(size%2) > maxWAVHeaderBytes {
			return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: decoded WAV header exceeds limit")
		}
		payload := make([]byte, int(size)+int(size%2))
		if _, err := io.ReadFull(input, payload); err != nil {
			return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: incomplete decoded WAV chunk")
		}
		headerBytes += len(payload)
		if string(chunk[:4]) == "fmt " {
			if format.Channels != 0 {
				return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: duplicate decoded WAV format")
			}
			var err error
			format, err = decodedWAVOutputFormat(payload[:size], output)
			if err != nil {
				return audio.Format{}, nil, err
			}
		}
	}
	return audio.Format{}, nil, fmt.Errorf("PROTOCOL_MISMATCH: decoded WAV has too many header chunks")
}

const mathMaxUint32 = ^uint32(0)

func decodedWAVFormat(payload []byte) (audio.Format, error) {
	return decodedWAVOutputFormat(payload, DecodeFloat32)
}

func decodedWAVOutputFormat(payload []byte, output string) (audio.Format, error) {
	invalid := fmt.Errorf("PROTOCOL_MISMATCH: unsupported decoded WAV format")
	if len(payload) < 16 {
		return audio.Format{}, invalid
	}
	channels := int(binary.LittleEndian.Uint16(payload[2:]))
	bits, code := 32, uint16(3)
	if output == DecodeSigned16 {
		bits, code = 16, 1
	}
	sampleBytes := bits / 8
	if channels < 1 || channels > 8 || binary.LittleEndian.Uint32(payload[4:]) != 48000 || binary.LittleEndian.Uint32(payload[8:]) != uint32(48000*channels*sampleBytes) || binary.LittleEndian.Uint16(payload[12:]) != uint16(channels*sampleBytes) || binary.LittleEndian.Uint16(payload[14:]) != uint16(bits) {
		return audio.Format{}, invalid
	}
	mask := uint32(0)
	switch binary.LittleEndian.Uint16(payload) {
	case code:
		// 无扩展头的单声道、立体声采用 WAV 的约定；多声道必须带掩码。
	case 0xfffe:
		pcmGUID := []byte{byte(code), 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71}
		if len(payload) < 40 || binary.LittleEndian.Uint16(payload[16:]) < 22 || int(binary.LittleEndian.Uint16(payload[16:]))+18 > len(payload) || binary.LittleEndian.Uint16(payload[18:]) != uint16(bits) || !bytes.Equal(payload[24:40], pcmGUID) {
			return audio.Format{}, invalid
		}
		mask = binary.LittleEndian.Uint32(payload[20:])
	default:
		return audio.Format{}, invalid
	}
	layout := ""
	switch {
	case channels == 1 && (mask == 0 || mask == 4):
		layout = "mono"
	case channels == 2 && (mask == 0 || mask == 3):
		layout = "stereo"
	case channels == 6 && mask == 0x3f:
		layout = "5.1"
	case channels == 6 && mask == 0x60f:
		layout = "5.1(side)"
	case channels == 8 && mask == 0x63f:
		layout = "7.1"
	default:
		return audio.Format{}, fmt.Errorf("UNSUPPORTED: decoded channel layout is unknown")
	}
	return audio.Format{SampleFormat: "f32le", SampleRate: 48000, Channels: channels, Layout: layout}, nil
}

type exactWAVReader struct {
	input     io.Reader
	remaining int64
}

func (r *exactWAVReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n, err := r.input.Read(p[:min(int64(len(p)), r.remaining)])
	r.remaining -= int64(n)
	if err == io.EOF && r.remaining != 0 {
		err = fmt.Errorf("PROTOCOL_MISMATCH: truncated decoded WAV payload")
	}
	return n, err
}
