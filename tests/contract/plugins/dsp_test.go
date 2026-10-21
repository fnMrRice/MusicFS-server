package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAudioDSPWireContract(t *testing.T) {
	request := audioffmpeg.StartRequest{
		Plan:       audioffmpeg.Plan{TaskID: "dsp-task", AssetID: "asset", Profile: "mp3-128", GainDB: -3},
		FFmpegPath: "C:/tools/ffmpeg.exe", FFmpegSHA256: strings.Repeat("a", 64),
		InputURL: "http://127.0.0.1:12345/" + strings.Repeat("b", 48) + "/input", OutputURL: "http://127.0.0.1:12345/" + strings.Repeat("b", 48) + "/output",
		DSPChain:   []audio.NodeSpec{{Name: "gain", Version: 1, Params: map[string]float64{"gainDB": -6}}, {Name: "musicfs.dsp.gain", Version: 1, Params: map[string]float64{"gainDB": -9}}, {Name: "bypass", Version: 1}},
		DSPPlugins: []dspworker.Config{{Name: "musicfs.dsp.gain", Executable: "C:/tools/dsp.exe", SHA256: strings.Repeat("c", 64), TimeoutMs: 5000}},
	}
	testkit.ValidatePlugin(t, "AudioStart", request)
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"audio.prepare", "audio.start"} {
		testkit.ValidatePlugin(t, "", pluginproto.Envelope{JSONRPC: "2.0", ID: "h-1", Method: method, Params: raw})
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["dspChain"].([]any)[0].(map[string]any)["version"] = 2 },
		func(v map[string]any) { v["dspChain"].([]any)[0].(map[string]any)["params"] = "invalid" },
		func(v map[string]any) { v["dspChain"].([]any)[0].(map[string]any)["pcm"] = "AAAA" },
		func(v map[string]any) { v["dspPlugins"].([]any)[0].(map[string]any)["sha256"] = "bad" },
		func(v map[string]any) { v["dspPlugins"].([]any)[0].(map[string]any)["name"] = "gain" },
		func(v map[string]any) { v["dspPlugins"].([]any)[0].(map[string]any)["timeoutMs"] = 1 },
		func(v map[string]any) {
			nodes := make([]any, 16)
			for i := range nodes {
				nodes[i] = map[string]any{"name": "bypass", "version": 1}
			}
			v["dspChain"] = nodes
		},
	} {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil {
			t.Fatal(err)
		}
		mutate(value)
		if err := testkit.ContractSchema(t, "plugin", "AudioStart").Validate(value); err == nil {
			t.Fatal("invalid DSP wire request accepted")
		}
	}
	request.DSPChain, request.DSPPlugins = nil, nil
	testkit.ValidatePlugin(t, "AudioStart", request)
}
