package audio_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestEffectsLogCanonicalIdentityAndPreserveNodeCapabilities(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: pluginlog.LevelTrace})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	registry := audio.NewRegistry()
	if err := builtin.Register(registry); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"equalizer", "compressor", "reverb", "musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb"} {
		node, err := registry.Create(name, nil)
		if err != nil {
			t.Fatal(err)
		}
		state, ok := node.(interface{ Stateless() bool })
		if !ok || state.Stateless() || node.DelayFrames() != 0 {
			t.Fatalf("node capabilities changed: %s", name)
		}
		if err = node.Configure(audio.Canonical); err != nil {
			t.Fatal(err)
		}
		block := &audio.Block{Format: audio.Canonical, Frames: 2, Samples: []float32{0.5, -0.5, 0.25, -0.25}}
		if err = node.Process(block); err != nil {
			t.Fatal(err)
		}
		if err = node.Process(&audio.Block{}); err == nil {
			t.Fatal("invalid block accepted")
		}
		for _, run := range []func() error{node.Reset, node.Drain, node.Close} {
			if err = run(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := registry.Create("equalizer", map[string]float64{"private-parameter-secret": 1}); err == nil {
		t.Fatal("invalid effect parameters accepted")
	}
	if strings.Contains(output.String(), "private-parameter-secret") {
		t.Fatal("raw parameter name leaked")
	}
	completed, failed := map[string]int{}, map[string]int{}
	decoder := json.NewDecoder(&output)
	for {
		var record map[string]any
		if err := decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		id, _ := record["pluginId"].(string)
		if record["msg"] == "DSP effect block completed" {
			if record["frames"] != float64(2) || record["sampleCount"] != float64(4) {
				t.Fatalf("unexpected counts: %v", record)
			}
			completed[id]++
		}
		if record["msg"] == "DSP effect block failed" {
			failed[id]++
		}
	}
	for _, id := range []string{"musicfs.dsp.equalizer", "musicfs.dsp.compressor", "musicfs.dsp.reverb"} {
		if completed[id] != 2 || failed[id] != 2 {
			t.Fatalf("missing plugin records for %s: completed=%d failed=%d", id, completed[id], failed[id])
		}
	}
}
