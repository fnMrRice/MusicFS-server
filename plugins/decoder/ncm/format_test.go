package ncmdecoder_test

import (
	"bytes"
	"testing"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func TestNCMProbeOwnsEncryptedContainerRecognition(t *testing.T) {
	r, err := audioformat.New(ncmdecoder.FormatDetector())
	if err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{"CTENFDAM", "CTENFDA", "ID3", "fLaC"} {
		match, ok, err := r.Detect(bytes.NewReader([]byte(input)), int64(len(input)))
		if err != nil || ok != (input == "CTENFDAM") {
			t.Fatalf("unexpected result for %q: %v %v", input, ok, err)
		}
		if ok && (match.Format != "ncm" || match.MIME != "audio/x-ncm" || !match.RequiresDecode) {
			t.Fatalf("incorrect container contract: %+v", match)
		}
	}
}
