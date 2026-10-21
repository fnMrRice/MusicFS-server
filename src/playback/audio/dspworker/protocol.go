// Package dspworker 实现有版本的外部 DSP 二进制 PCM 协议。
package dspworker

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

const ProtocolVersion = 1
const maxPayload = 64 * 1024
const (
	frameHello uint32 = iota + 1
	frameProcess
	frameReset
	frameDrain
	frameError
)

type hello struct {
	ProtocolVersion int                `json:"protocolVersion"`
	Name            string             `json:"name"`
	NodeVersion     int                `json:"nodeVersion"`
	Format          audio.Format       `json:"format"`
	Params          map[string]float64 `json:"params"`
}

type ready struct {
	ProtocolVersion int          `json:"protocolVersion"`
	PluginVersion   string       `json:"pluginVersion"`
	Name            string       `json:"name"`
	NodeVersion     int          `json:"nodeVersion"`
	Format          audio.Format `json:"format"`
	DelayFrames     int64        `json:"delayFrames"`
	TailFrames      int64        `json:"tailFrames"`
	PreservesTiming bool         `json:"preservesTiming"`
	Stateless       bool         `json:"stateless"`
}

func writeFrame(w io.Writer, kind uint32, data []byte) error {
	if len(data) > maxPayload {
		return fmt.Errorf("PROTOCOL_MISMATCH: oversized DSP frame")
	}
	header := [12]byte{'M', 'F', 'D', '1'}
	binary.LittleEndian.PutUint32(header[4:], kind)
	binary.LittleEndian.PutUint32(header[8:], uint32(len(data)))
	for _, part := range [][]byte{header[:], data} {
		for len(part) > 0 {
			n, err := w.Write(part)
			if err != nil {
				return err
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}

func readFrame(r io.Reader) (uint32, []byte, error) {
	var header [12]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, nil, err
	}
	size := binary.LittleEndian.Uint32(header[8:])
	if string(header[:4]) != "MFD1" || size > maxPayload {
		return 0, nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid DSP frame")
	}
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	return binary.LittleEndian.Uint32(header[4:]), data, err
}

func encodeBlock(block *audio.Block) ([]byte, error) {
	if block.Format != audio.Canonical {
		return nil, fmt.Errorf("UNSUPPORTED: DSP v1 requires stereo PCM")
	}
	if err := block.Validate(); err != nil {
		return nil, err
	}
	data := make([]byte, 24+len(block.Samples)*4)
	binary.LittleEndian.PutUint64(data, block.Sequence)
	binary.LittleEndian.PutUint64(data[8:], uint64(block.PositionFrames))
	binary.LittleEndian.PutUint32(data[16:], uint32(block.Frames))
	if block.End {
		data[20] |= 1
	}
	if block.Discontinuity {
		data[20] |= 2
	}
	for i, sample := range block.Samples {
		if math.IsNaN(float64(sample)) || math.IsInf(float64(sample), 0) {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: non-finite DSP sample")
		}
		binary.LittleEndian.PutUint32(data[24+i*4:], math.Float32bits(sample))
	}
	return data, nil
}

func decodeBlock(data []byte) (*audio.Block, error) {
	if len(data) < 24 || data[20] > 3 || data[21] != 0 || data[22] != 0 || data[23] != 0 {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid DSP block")
	}
	frames := int(binary.LittleEndian.Uint32(data[16:]))
	if frames > 4096 || len(data) != 24+frames*8 {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid DSP block size")
	}
	block := &audio.Block{Format: audio.Canonical, Frames: frames, Sequence: binary.LittleEndian.Uint64(data), PositionFrames: int64(binary.LittleEndian.Uint64(data[8:])), End: data[20]&1 != 0, Discontinuity: data[20]&2 != 0, Samples: make([]float32, frames*2)}
	for i := range block.Samples {
		block.Samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[24+i*4:]))
		if math.IsNaN(float64(block.Samples[i])) || math.IsInf(float64(block.Samples[i]), 0) {
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: non-finite DSP sample")
		}
	}
	return block, nil
}

func marshal(value any) []byte {
	data, _ := json.Marshal(value)
	return data
}
