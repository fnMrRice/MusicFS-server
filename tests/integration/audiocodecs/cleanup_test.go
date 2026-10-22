package audiocodecs_test

import (
	"context"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func TestNCMChainFailuresReleaseIntermediateResources(t *testing.T) {
	ncm, ncmGate := ncmProcessor(t)
	ffmpeg, audioGate, workDir, _ := audioProcessor(t)
	chain, err := audioformat.NewChain(ncm.AudioDecoder(), ffmpeg.AudioDecoder())
	if err != nil {
		t.Fatal(err)
	}
	_, input := ncmFixture(t, "lossless.flac")
	for _, sample := range []struct {
		name, format string
		limit        int64
	}{
		{"unsupported output", "not-a-decoder-output", 16 << 20},
		{"output budget", audioffmpeg.DecodeFloat32, 64},
	} {
		t.Run(sample.name, func(t *testing.T) {
			request := decodeRequest(sample.format)
			request.MaxOutputBytes = sample.limit
			if decoded, err := chain.Decode(t.Context(), request, input); err == nil || decoded != nil {
				if decoded != nil {
					_ = decoded.Close()
				}
				t.Fatalf("invalid decode succeeded: %v", err)
			}
			if ncmGate.Active() != 0 || audioGate.Active() != 0 {
				t.Fatal("failed chain retained runtime leases", ncmGate.Active(), audioGate.Active())
			}
			assertNoCodecFiles(t, workDir)
			// 同一实例只有一个并发槽；失败后能立即重开才算完整释放中间层。
			again, err := chain.Decode(t.Context(), decodeRequest("native"), input)
			if err != nil {
				t.Fatalf("failed chain did not free its decoder slot: %v", err)
			}
			if err := again.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNCMChainCancellationClosesTemporaryOutput(t *testing.T) {
	ncm, ncmGate := ncmProcessor(t)
	ffmpeg, audioGate, workDir, _ := audioProcessor(t)
	chain, err := audioformat.NewChain(ncm.AudioDecoder(), ffmpeg.AudioDecoder())
	if err != nil {
		t.Fatal(err)
	}
	_, input := ncmFixture(t, "lossless.flac")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	decoded, err := chain.Decode(ctx, decodeRequest(audioffmpeg.DecodeFloat32), input)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = decoded.Close() })
	readOutput(t, decoded.Input)
	cancel()
	if err := decoded.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := decoded.Reader.ReadAt(make([]byte, 4), 0); err == nil || ncmGate.Active() != 0 || audioGate.Active() != 0 {
		t.Fatal("canceled chain retained output or runtime leases", err, ncmGate.Active(), audioGate.Active())
	}
	assertNoCodecFiles(t, workDir)
	again, err := chain.Decode(t.Context(), decodeRequest(audioffmpeg.DecodeSigned16), input)
	if err != nil {
		t.Fatalf("canceled chain did not release its decoder slots: %v", err)
	}
	if err := again.Close(); err != nil {
		t.Fatal(err)
	}
	assertNoCodecFiles(t, workDir)
}
