package api_test

import (
	"encoding/json"
	"strings"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestCodecOutputSelectionContract(t *testing.T) {
	for _, format := range []string{audioffmpeg.DecodeFloat32, audioffmpeg.DecodeSigned16} {
		plan := audioffmpeg.Plan{TaskID: "task", AssetID: "asset", Profile: "mp3-128", DecodeOutput: format}
		testkit.ValidatePlugin(t, "AudioPlan", plan)
	}
	request := audioffmpeg.CodecRequest{TaskID: "task", AssetID: "asset", OutputFormat: audioffmpeg.DecodeSigned16, MaxOutputBytes: audioffmpeg.DefaultDecodeOutputBytes,
		FFmpegPath: "C:/tools/ffmpeg.exe", FFmpegSHA256: strings.Repeat("a", 64), InputURL: "http://127.0.0.1:12345/" + strings.Repeat("b", 48) + "/input", OutputURL: "http://127.0.0.1:12345/" + strings.Repeat("b", 48) + "/output"}
	testkit.ValidatePlugin(t, "AudioCodecRequest", request)
	for _, value := range []audioffmpeg.CodecRequest{
		{TaskID: "task", AssetID: "asset", OutputFormat: "exec"},
		func() audioffmpeg.CodecRequest {
			bad := request
			bad.MaxOutputBytes = audioffmpeg.MaxDecodeOutputBytes + 1
			return bad
		}(),
		func() audioffmpeg.CodecRequest {
			bad := request
			bad.InputURL = "https://outside.invalid/input"
			return bad
		}(),
	} {
		raw, _ := json.Marshal(value)
		if testkit.ContractSchema(t, "plugin", "AudioCodecRequest").Validate(testkit.DecodeJSON(t, raw)) == nil {
			t.Fatal("invalid codec request passed schema")
		}
	}
}
