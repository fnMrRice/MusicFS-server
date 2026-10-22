package audio_test

import (
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspchain"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
)

func TestChannelMixConfigurationAndAliasesAreReserved(t *testing.T) {
	for _, name := range []string{"channelmix", "musicfs.dsp.channelmix", "musicfs.plugin.decoder.channelmix"} {
		for _, channels := range []float64{1, 2, 6, 8} {
			chain := []audio.NodeSpec{{Name: name, Version: audio.NodeVersion, Params: map[string]float64{"outputChannels": channels}}}
			if err := dspchain.Validate(chain, nil); err != nil {
				t.Fatalf("channel conversion configuration rejected: %s %g %v", name, channels, err)
			}
		}
		plugin := dspworker.Config{Name: name, Executable: "C:/plugins/dsp.exe", SHA256: "0000000000000000000000000000000000000000000000000000000000000000"}
		if err := plugin.Validate(); err == nil {
			t.Fatalf("external plugin replaced builtin name %s", name)
		}
	}
	for _, params := range []map[string]float64{{"outputChannels": 3}, {"layout": 6}} {
		if err := dspchain.Validate([]audio.NodeSpec{{Name: "channelmix", Version: audio.NodeVersion, Params: params}}, nil); err == nil {
			t.Fatalf("invalid channel conversion parameters accepted: %+v", params)
		}
	}
}
