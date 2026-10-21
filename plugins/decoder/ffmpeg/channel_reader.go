package audioffmpeg

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

type channelReader struct {
	input     io.Reader
	converter *loggedChannelConverter
	channels  int
	buffer    []byte
	pending   []byte
	readErr   error
}

// convertedWAVReader 保留源 WAV 布局，交给独立转换插件后输出目标交错 PCM。
func convertedWAVReader(input io.Reader, target audio.Format) (io.Reader, error) {
	source, pcm, err := readDecodedWAV(input)
	if err != nil {
		return nil, err
	}
	converter, err := newLoggedChannelConverter(source, target)
	if err != nil {
		return nil, err
	}
	return &channelReader{input: pcm, converter: converter, channels: source.Channels, buffer: make([]byte, 4096*source.Channels*4)}, nil
}

func (r *channelReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if r.readErr != nil {
			return 0, r.readErr
		}
		n, err := io.ReadFull(r.input, r.buffer)
		if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
			return 0, err
		}
		if n%(r.channels*4) != 0 {
			return 0, fmt.Errorf("PROTOCOL_MISMATCH: unaligned decoded WAV payload")
		}
		if err != nil {
			r.readErr = io.EOF
		}
		if n == 0 {
			return 0, r.readErr
		}
		samples := make([]float32, n/4)
		for i := range samples {
			samples[i] = math.Float32frombits(binary.LittleEndian.Uint32(r.buffer[i*4:]))
		}
		converted, err := r.converter.Process(samples)
		if err != nil {
			return 0, err
		}
		r.pending = make([]byte, len(converted)*4)
		for i, sample := range converted {
			binary.LittleEndian.PutUint32(r.pending[i*4:], math.Float32bits(sample))
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
