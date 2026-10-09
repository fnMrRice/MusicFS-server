package audio

import "fmt"

type Format struct {
	SampleFormat string `json:"sampleFormat"`
	SampleRate   int    `json:"sampleRate"`
	Channels     int    `json:"channels"`
	Layout       string `json:"layout"`
}

var Canonical = Format{SampleFormat: "f32le", SampleRate: 48000, Channels: 2, Layout: "stereo"}

type Block struct {
	Format         Format
	Samples        []float32
	Frames         int
	Sequence       uint64
	PositionFrames int64
	End            bool
	Discontinuity  bool
}

func (b Block) Validate() error {
	if b.Format != Canonical || b.Frames < 0 || b.Frames > 4096 || len(b.Samples) != b.Frames*b.Format.Channels {
		return fmt.Errorf("UNSUPPORTED: invalid PCM block")
	}
	return nil
}
