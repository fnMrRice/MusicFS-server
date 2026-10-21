package audio

import "fmt"

type Format struct {
	SampleFormat string `json:"sampleFormat"`
	SampleRate   int    `json:"sampleRate"`
	Channels     int    `json:"channels"`
	Layout       string `json:"layout"`
}

var Canonical = Format{SampleFormat: "f32le", SampleRate: 48000, Channels: 2, Layout: "stereo"}

// FormatForLayout 返回服务器支持的交错 PCM 格式；空布局沿用立体声默认值。
// 5.1 声道顺序为 FL、FR、FC、LFE、BL、BR；7.1 末尾追加 SL、SR。
func FormatForLayout(layout string) (Format, error) {
	if layout == "" {
		layout = "stereo"
	}
	channels := 0
	switch layout {
	case "mono":
		channels = 1
	case "stereo":
		channels = 2
	case "5.1":
		channels = 6
	case "7.1":
		channels = 8
	default:
		return Format{}, fmt.Errorf("UNSUPPORTED: unsupported PCM layout %q", layout)
	}
	return Format{SampleFormat: "f32le", SampleRate: 48000, Channels: channels, Layout: layout}, nil
}

func (f Format) Validate() error {
	expected, err := FormatForLayout(f.Layout)
	if err != nil || f.Layout == "" || f != expected {
		return fmt.Errorf("UNSUPPORTED: unsupported PCM format")
	}
	return nil
}

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
	if b.Format.Validate() != nil || b.Frames < 0 || b.Frames > 4096 || len(b.Samples) != b.Frames*b.Format.Channels {
		return fmt.Errorf("UNSUPPORTED: invalid PCM block")
	}
	return nil
}
