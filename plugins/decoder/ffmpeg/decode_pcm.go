package audioffmpeg

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// decodedPCMReader 仅调整解码输出的采样表示；声道转换仍交给 DSP 插件。
func decodedPCMReader(input io.Reader, output string) io.Reader {
	if output == DecodeSigned16 {
		return &signed16FloatReader{input: input}
	}
	return input
}

type signed16FloatReader struct {
	input   io.Reader
	pending []byte
	err     error
}

func (r *signed16FloatReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.pending) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		var data [8192]byte
		n, err := io.ReadFull(r.input, data[:])
		if n%2 != 0 {
			return 0, fmt.Errorf("PROTOCOL_MISMATCH: unaligned signed PCM payload")
		}
		if err != nil {
			if err == io.ErrUnexpectedEOF {
				err = io.EOF
			}
			r.err = err
		}
		if n == 0 {
			return 0, r.err
		}
		r.pending = make([]byte, n*2)
		for at := 0; at < n; at += 2 {
			sample := float32(int16(binary.LittleEndian.Uint16(data[at:]))) / 32768
			binary.LittleEndian.PutUint32(r.pending[at*2:], math.Float32bits(sample))
		}
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
}
