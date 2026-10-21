package audioffmpeg

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func wavFormatChunk(channels int, mask uint32) []byte {
	format := make([]byte, 40)
	binary.LittleEndian.PutUint16(format, 0xfffe)
	binary.LittleEndian.PutUint16(format[2:], uint16(channels))
	binary.LittleEndian.PutUint32(format[4:], 48000)
	binary.LittleEndian.PutUint32(format[8:], uint32(48000*channels*4))
	binary.LittleEndian.PutUint16(format[12:], uint16(channels*4))
	binary.LittleEndian.PutUint16(format[14:], 32)
	binary.LittleEndian.PutUint16(format[16:], 22)
	binary.LittleEndian.PutUint16(format[18:], 32)
	binary.LittleEndian.PutUint32(format[20:], mask)
	copy(format[24:], []byte{3, 0, 0, 0, 0, 0, 0x10, 0, 0x80, 0, 0, 0xaa, 0, 0x38, 0x9b, 0x71})
	return format
}

func wavChunk(name string, data []byte) []byte {
	var output bytes.Buffer
	output.WriteString(name)
	binary.Write(&output, binary.LittleEndian, uint32(len(data)))
	output.Write(data)
	if len(data)%2 != 0 {
		output.WriteByte(0)
	}
	return output.Bytes()
}

func testWAV(channels int, mask uint32, samples []float32) []byte {
	var pcm bytes.Buffer
	for _, sample := range samples {
		binary.Write(&pcm, binary.LittleEndian, math.Float32bits(sample))
	}
	var chunks bytes.Buffer
	chunks.Write(wavChunk("fmt ", wavFormatChunk(channels, mask)))
	chunks.Write(wavChunk("JUNK", []byte{1, 2, 3}))
	chunks.Write(wavChunk("data", pcm.Bytes()))
	var wave bytes.Buffer
	wave.WriteString("RIFF")
	binary.Write(&wave, binary.LittleEndian, uint32(4+chunks.Len()))
	wave.WriteString("WAVE")
	wave.Write(chunks.Bytes())
	return wave.Bytes()
}

func TestDecodedWAVPreservesExplicitSpeakerMask(t *testing.T) {
	for _, tc := range []struct {
		channels int
		mask     uint32
		layout   string
	}{{1, 4, "mono"}, {2, 3, "stereo"}, {6, 0x3f, "5.1"}, {6, 0x60f, "5.1(side)"}, {8, 0x63f, "7.1"}} {
		t.Run(tc.layout, func(t *testing.T) {
			samples := make([]float32, tc.channels*2)
			samples[0] = 0.5
			data := testWAV(tc.channels, tc.mask, samples)
			for _, streaming := range []bool{false, true} {
				if streaming {
					binary.LittleEndian.PutUint32(data[4:], mathMaxUint32)
					at := bytes.Index(data, []byte("data"))
					binary.LittleEndian.PutUint32(data[at+4:], mathMaxUint32)
				}
				format, pcm, err := readDecodedWAV(boundedReader{bytes.NewReader(data), 3})
				if err != nil {
					t.Fatal(err)
				}
				payload, err := io.ReadAll(pcm)
				if err != nil || format.Layout != tc.layout || format.Channels != tc.channels || len(payload) != len(samples)*4 {
					t.Fatalf("format=%+v bytes=%d error=%v", format, len(payload), err)
				}
			}
		})
	}
}

func TestWAVReaderRejectsUntrustedFormatsAndBoundedHeaders(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{"unknown mask", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[40:], 0); return b }},
		{"wrong mask", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[40:], 0x63f); return b }},
		{"integer samples", func(b []byte) []byte { b[44] = 1; return b }},
		{"sample rate", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[24:], 44100); return b }},
		{"block alignment", func(b []byte) []byte { b[32] = 1; return b }},
		{"oversized chunk", func(b []byte) []byte { binary.LittleEndian.PutUint32(b[16:], maxWAVHeaderBytes+1); return b }},
		{"truncated header", func(b []byte) []byte { return b[:25] }},
		{"non-WAV", func(b []byte) []byte { b[8] = 'X'; return b }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wave := tc.edit(testWAV(6, 0x3f, make([]float32, 6)))
			if _, _, err := readDecodedWAV(bytes.NewReader(wave)); err == nil {
				t.Fatal("invalid WAV accepted")
			}
		})
	}
}

func TestConvertedWAVReaderPreservesSurroundAndRejectsTruncation(t *testing.T) {
	format, err := audio.FormatForLayout("7.1")
	if err != nil {
		t.Fatal(err)
	}
	wave := testWAV(6, 0x60f, []float32{0, 0, 0, 0, 0.75, -0.5})
	reader, err := convertedWAVReader(bytes.NewReader(wave), format)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	if err != nil || len(got) != 32 {
		t.Fatalf("bytes=%d error=%v", len(got), err)
	}
	for channel, want := range []float32{0, 0, 0, 0, 0, 0, 0.75, -0.5} {
		actual := math.Float32frombits(binary.LittleEndian.Uint32(got[channel*4:]))
		if actual != want {
			t.Fatalf("channel=%d got=%g want=%g", channel, actual, want)
		}
	}
	reader, err = convertedWAVReader(bytes.NewReader(wave[:len(wave)-4]), format)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadAll(reader); err == nil {
		t.Fatal("truncated declared data accepted")
	}
}
