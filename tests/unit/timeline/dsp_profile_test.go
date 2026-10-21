package timeline_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/playback/timeline"
)

func TestDSPAlignmentRequiresExactProcessingChain(t *testing.T) {
	profile := timeline.Profile{OutputProfile: "mp3-128", ProcessingRevision: "dsp-v1:" + strings.Repeat("a", 64), DecoderSHA256: strings.Repeat("b", 64), GainDB: -3}
	request := timeline.Request{RecordingID: "recording", From: timeline.Target{SourceTrackID: "from", AssetID: "asset-from", Version: "v1"}, To: timeline.Target{SourceTrackID: "to", AssetID: "asset-to", Version: "v1"}, Range: timeline.Range{EndMs: 6000}, OffsetMs: 1000, Profile: profile}
	evidence := timeline.Evidence{Request: request, Origin: "manualAlignment", Revision: 1}
	check := timeline.Check{From: request.From, To: request.To, PositionMs: 4000, Profile: profile}
	if position, err := evidence.Map(check); err != nil || position != 5000 {
		t.Fatalf("matching DSP chain alignment failed: position=%d err=%v", position, err)
	}
	for _, revision := range []string{"gain-v1", "dsp-v1:" + strings.Repeat("c", 64)} {
		check.Profile.ProcessingRevision = revision
		if _, err := evidence.Map(check); err == nil {
			t.Fatal("different DSP chain reused timeline evidence")
		}
	}
	for _, revision := range []string{"", "dsp-v1:", "dsp-v1:" + strings.Repeat("A", 64), "dsp-v1:" + strings.Repeat("g", 64), "dsp-v2:" + strings.Repeat("a", 64)} {
		candidate := profile
		candidate.ProcessingRevision = revision
		if err := candidate.Validate(); err == nil {
			t.Fatalf("invalid DSP chain identity accepted: %q", revision)
		}
	}
}
