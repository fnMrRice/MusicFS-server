package audioffmpeg

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/channelmix"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

func TestChannelReaderLogsSeparatePluginIdentity(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: pluginlog.LevelTrace})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	target, err := audio.FormatForLayout("7.1")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := convertedWAVReader(bytes.NewReader(testWAV(6, 0x60f, []float32{0, 0, 0, 0, 0.75, -0.5})), target)
	if err != nil {
		t.Fatal(err)
	}
	pcm, err := io.ReadAll(reader)
	if err != nil || len(pcm) != 32 {
		t.Fatalf("conversion failed: bytes=%d err=%v", len(pcm), err)
	}
	decoder := json.NewDecoder(&output)
	var ready, completed bool
	for {
		var record map[string]any
		if err = decoder.Decode(&record); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if record["pluginId"] != channelmix.PluginID {
			t.Fatalf("wrong plugin scope: %v", record)
		}
		ready = ready || record["msg"] == "Channel converter ready" && record["level"] == "DEBUG"
		completed = completed || record["msg"] == "Channel conversion completed" && record["frames"] == float64(1) && record["inputSamples"] == float64(6) && record["outputSamples"] == float64(8)
	}
	if !ready || !completed {
		t.Fatalf("missing conversion diagnostics: ready=%v completed=%v", ready, completed)
	}
}
