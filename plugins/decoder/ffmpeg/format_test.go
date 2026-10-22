package audioffmpeg_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func TestPluginRecognizesFormatsWithoutExtensions(t *testing.T) {
	registry, err := audioformat.New(audioffmpeg.FormatDetector())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct{ name, content, format string }{
		{"MP3 ID3", "ID3\x04\x00", "mp3"},
		{"MP3 frame", "\xff\xfb\x90\x64", "mp3"},
		{"AAC ADTS", "\xff\xf1\x50\x80", "aac"},
		{"AAC MPEG2", "\xff\xf9\x50\x80", "aac"},
		{"FLAC", "fLaC", "flac"},
		{"Ogg", "OggS", "ogg"},
		{"M4A", "\x00\x00\x00\x20ftypM4A ", "mp4"},
		{"AIFF", "FORM\x00\x00\x00\x20AIFF", "aiff"},
		{"WebM", "\x1a\x45\xdf\xa3\x87\x42\x82\x84webm", "webm"},
		{"AVIF image", "\x00\x00\x00\x20ftypavif", ""},
		{"unrelated EBML", "\x1a\x45\xdf\xa3\x87\x42\x82\x84xxxx", ""},
		{"NCM", "CTENFDAM", ""},
		{"unknown", "not audio", ""},
	} {
		t.Run(sample.name, func(t *testing.T) {
			match, ok, err := registry.Detect(bytes.NewReader([]byte(sample.content)), int64(len(sample.content)))
			if err != nil || ok != (sample.format != "") || match.Format != sample.format || match.RequiresDecode {
				t.Fatalf("unexpected match: %+v %v %v", match, ok, err)
			}
		})
	}
}

func TestWaveDurationRemainsPluginMetadata(t *testing.T) {
	header := make([]byte, 44)
	copy(header, "RIFF")
	copy(header[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(header[16:], 16)
	binary.LittleEndian.PutUint32(header[28:], 32000)
	copy(header[36:], "data")
	binary.LittleEndian.PutUint32(header[40:], 64000)
	match, ok := audioffmpeg.FormatDetector().Probe(header, 64044)
	if !ok || match.DurationMs == nil || *match.DurationMs != 2000 {
		t.Fatalf("incorrect duration: %+v", match)
	}
	match, ok = audioffmpeg.FormatDetector().Probe(header, 44)
	if !ok || match.DurationMs != nil {
		t.Fatalf("truncated data must not publish duration: %+v", match)
	}
}
