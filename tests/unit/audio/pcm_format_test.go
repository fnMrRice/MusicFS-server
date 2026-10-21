package audio_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestSupportedPCMFormatsAndBlocks(t *testing.T) {
	for _, item := range []struct {
		layout   string
		channels int
	}{
		{"", 2}, {"mono", 1}, {"stereo", 2}, {"5.1", 6}, {"7.1", 8},
	} {
		format, err := audio.FormatForLayout(item.layout)
		if err != nil {
			t.Fatal(err)
		}
		if format.SampleFormat != "f32le" || format.SampleRate != 48000 || format.Channels != item.channels {
			t.Fatalf("incorrect format for %q: %+v", item.layout, format)
		}
		if item.layout == "" && format != audio.Canonical {
			t.Fatal("default format changed")
		}
		if err := format.Validate(); err != nil {
			t.Fatal(err)
		}
		for _, frames := range []int{0, 1, 4096} {
			block := audio.Block{Format: format, Frames: frames, Samples: make([]float32, frames*item.channels)}
			if err := block.Validate(); err != nil {
				t.Fatal(err)
			}
		}
		wrong := format
		wrong.Channels++
		if err := wrong.Validate(); err == nil {
			t.Fatal("accepted mismatched channel count")
		}
	}
}

func TestUnsupportedPCMFormatsAndMalformedBlocks(t *testing.T) {
	for _, layout := range []string{"5.1(side)", "7.1(wide)", "quad", "Stereo", "stereo "} {
		if _, err := audio.FormatForLayout(layout); err == nil {
			t.Fatalf("accepted layout %q", layout)
		}
	}
	for _, format := range []audio.Format{
		{},
		{SampleFormat: "f32le", SampleRate: 48000, Channels: 2},
		{SampleFormat: "f32le", SampleRate: 44100, Channels: 2, Layout: "stereo"},
		{SampleFormat: "s16le", SampleRate: 48000, Channels: 6, Layout: "5.1"},
		{SampleFormat: "f32le", SampleRate: 48000, Channels: 6, Layout: "7.1"},
	} {
		if err := format.Validate(); err == nil {
			t.Fatalf("accepted format %+v", format)
		}
		if err := (audio.Block{Format: format}).Validate(); err == nil {
			t.Fatal("block accepted unsupported format")
		}
	}
	format, _ := audio.FormatForLayout("7.1")
	for _, block := range []audio.Block{
		{Format: format, Frames: -1},
		{Format: format, Frames: 4097, Samples: make([]float32, 4097*8)},
		{Format: format, Frames: 1, Samples: make([]float32, 2)},
		{Format: format, Frames: 1, Samples: make([]float32, 9)},
	} {
		if err := block.Validate(); err == nil {
			t.Fatal("accepted malformed multichannel block")
		}
	}
}
