package audioffmpeg

import (
	"strings"
	"testing"
)

func TestAudioComponentProbeRequiresWaveOutputAndKeepsOpusOptional(t *testing.T) {
	encoders := " A libmp3lame MP3\n A pcm_f32le PCM\n"
	decoders := " A pcm_f32le PCM\n"
	demuxers := " D wav Wave\n D f32le PCM\n"
	muxers := " E mp3 MP3\n E wav Wave\n"
	for _, tc := range []struct {
		encoders string
		muxers   string
		want     int
	}{
		{encoders, muxers, 1},
		{encoders + " A libopus Opus\n", muxers, 1},
		{encoders, muxers + " E webm WebM\n", 1},
		{encoders + " A libopus Opus\n", muxers + " E webm WebM\n", 3},
		{encoders, strings.ReplaceAll(muxers, " E wav Wave\n", ""), 0},
	} {
		profiles, err := profilesForComponents(tc.encoders, decoders, demuxers, tc.muxers)
		if tc.want == 0 {
			if err == nil {
				t.Fatal("probe accepted a missing WAV muxer")
			}
			continue
		}
		if err != nil || len(profiles) != tc.want || profiles[0].ID != "mp3-128" {
			t.Fatal("optional Opus support changed the baseline MP3 capability", profiles, err)
		}
	}
}

func TestOutputProfileRequiresTheConfiguredEncodingCapability(t *testing.T) {
	stereo, _ := ProfileByID("mp3-128")
	surround51, _ := ProfileByID("opus-5.1")
	surround71, _ := ProfileByID("opus-7.1")
	for _, tc := range []struct {
		layout   string
		profiles []Profile
		want     string
	}{
		{"", []Profile{stereo}, "mp3-128"},
		{"stereo", []Profile{stereo}, "mp3-128"},
		{"5.1", []Profile{stereo}, ""},
		{"7.1", []Profile{stereo, surround51}, ""},
		{"5.1", []Profile{stereo, surround51, surround71}, "opus-5.1"},
		{"7.1", []Profile{stereo, surround51, surround71}, "opus-7.1"},
		{"unknown", []Profile{stereo}, ""},
		{"", nil, ""},
	} {
		processor := &Processor{config: Config{OutputLayout: tc.layout}, Capabilities: Capabilities{Profiles: tc.profiles}}
		profile, err := processor.OutputProfile()
		if tc.want == "" {
			if err == nil {
				t.Fatalf("missing configured profile accepted: %q", tc.layout)
			}
			continue
		}
		if err != nil || profile.ID != tc.want || processor.RequiresChannelConversion() != (tc.layout == "5.1" || tc.layout == "7.1") {
			t.Fatalf("unexpected configured output: %+v %v", profile, err)
		}
	}
	if _, ok := ProfileByID("opus-stereo"); ok {
		t.Fatal("undefined output profile accepted")
	}
}
