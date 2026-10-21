package builtinruntime_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestBuiltinAudioToolPipelineWithBuiltinDSP(t *testing.T) {
	ffmpeg, ffprobe := os.Getenv("MUSICFS_FFMPEG"), os.Getenv("MUSICFS_FFPROBE")
	if ffmpeg == "" || ffprobe == "" {
		t.Skip("set MUSICFS_FFMPEG and MUSICFS_FFPROBE to run actual audio tool integration")
	}
	processor, err := audioffmpeg.NewProcessor(t.Context(), audioffmpeg.Config{
		Builtin: true, FFmpegPath: ffmpeg, FFprobePath: ffprobe,
		DSPChain:   []audio.NodeSpec{{Name: "musicfs.dsp.gain", Version: audio.NodeVersion, Params: map[string]float64{"gainDB": -6}}},
		DSPPlugins: []dspworker.Config{{Builtin: true, Name: "musicfs.dsp.gain"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	err = processor.Process(t.Context(), audioffmpeg.Plan{TaskID: "builtin-audio", AssetID: "asset", Profile: "mp3-128"}, bytes.NewReader(input), int64(len(input)), &output)
	if err != nil || output.Len() == 0 {
		t.Fatal("builtin audio and DSP pipeline produced no output", err)
	}
	if processor.PluginVersion() != bundledplugin.FFmpegVersion || processor.DSPPluginVersion("musicfs.dsp.gain") != bundledplugin.GainVersion {
		t.Fatal("builtin pipeline did not verify plugin versions")
	}
}
