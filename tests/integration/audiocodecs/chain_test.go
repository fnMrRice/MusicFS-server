package audiocodecs_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	audioffmpeg "github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func TestNCMNativeChainPreservesEmbeddedAudio(t *testing.T) {
	decoder, gate := ncmProcessor(t)
	var native *audioffmpeg.Processor
	chain, err := audioformat.NewChain(native.AudioDecoder(), decoder.AudioDecoder())
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct{ file, format, mime string }{
		{"vbr-untagged.mp3", "mp3", "audio/mpeg"},
		{"lossless.flac", "flac", "audio/flac"},
	} {
		t.Run(sample.format, func(t *testing.T) {
			plain, input := ncmFixture(t, sample.file)
			decoded, err := chain.Decode(t.Context(), decodeRequest("native"), input)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = decoded.Close() })
			if !reflect.DeepEqual(decoded.Layers, []string{"musicfs.plugin.decoder.ncm", "musicfs.plugin.decoder.ffmpeg"}) {
				t.Fatal("incorrect decoder order", decoded.Layers)
			}
			if decoded.Format != sample.format || decoded.MIME != sample.mime || decoded.Metadata.Title != "链式解码测试" || !bytes.Equal(readOutput(t, decoded.Input), plain) {
				t.Fatal("native chain altered audio or lost verified format and metadata")
			}
			if gate.Active() == 0 {
				t.Fatal("native result lost its input lease before close")
			}
			if err := decoded.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := decoded.Reader.ReadAt(make([]byte, 4), 0); err == nil || gate.Active() != 0 {
				t.Fatal("closed native chain retained its reader or runtime lease", err, gate.Active())
			}
		})
	}
}

func TestNCMChainProducesSelectedPCMAndReleasesBothPlugins(t *testing.T) {
	ncm, ncmGate := ncmProcessor(t)
	ffmpeg, audioGate, workDir, ffprobe := audioProcessor(t)
	chain, err := audioformat.NewChain(ffmpeg.AudioDecoder(), ncm.AudioDecoder())
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"vbr-untagged.mp3", "lossless.flac"} {
		for _, format := range []string{audioffmpeg.DecodeFloat32, audioffmpeg.DecodeSigned16} {
			t.Run(source+"/"+format, func(t *testing.T) {
				plain, input := ncmFixture(t, source)
				decoded, err := chain.Decode(t.Context(), decodeRequest(format), input)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = decoded.Close() })
				if decoded.Format != "wav" || decoded.MIME != "audio/wav" || !reflect.DeepEqual(decoded.Layers, []string{"musicfs.plugin.decoder.ncm", "musicfs.plugin.decoder.ffmpeg"}) {
					t.Fatal("invalid terminal PCM representation", decoded.Format, decoded.MIME, decoded.Layers)
				}
				actual := readOutput(t, decoded.Input)
				assertPCMFormat(t, ffprobe, actual, format)
				if err := decoded.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := decoded.Reader.ReadAt(make([]byte, 4), 0); err == nil || ncmGate.Active() != 0 || audioGate.Active() != 0 {
					t.Fatal("closed PCM chain retained its output or runtime leases", err, ncmGate.Active(), audioGate.Active())
				}
				assertNoCodecFiles(t, workDir)
				unwrapped, err := chain.Decode(t.Context(), decodeRequest(format), audioformat.Input{Reader: bytes.NewReader(plain), Size: int64(len(plain))})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = unwrapped.Close() })
				if !reflect.DeepEqual(unwrapped.Layers, []string{"musicfs.plugin.decoder.ffmpeg"}) || !bytes.Equal(readOutput(t, unwrapped.Input), actual) {
					t.Fatal("encrypted and ordinary inputs produced different PCM")
				}
				if err := unwrapped.Close(); err != nil {
					t.Fatal(err)
				}
				assertNoCodecFiles(t, workDir)
			})
		}
	}
}

func TestUnavailableNCMDoesNotBypassContainerDecoder(t *testing.T) {
	_, input := ncmFixture(t, "lossless.flac")
	var unavailable *ncmdecoder.Processor
	fallbackCalls := 0
	fallback := audioformat.Decoder{
		Detector: audioformat.Detector{PluginID: "fallback", Priority: -1, Probe: func([]byte, int64) (audioformat.Match, bool) {
			return audioformat.Match{Format: "opaque", MIME: "audio/opaque"}, true
		}},
		Decode: func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			fallbackCalls++
			return audioformat.Output{}, nil
		},
	}
	chain, err := audioformat.NewChain(fallback, unavailable.AudioDecoder())
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := chain.Decode(t.Context(), decodeRequest("native"), input); err == nil || decoded != nil || fallbackCalls != 0 {
		t.Fatalf("unavailable container decoder was bypassed: output=%v error=%v fallback=%d", decoded, err, fallbackCalls)
	}
}
