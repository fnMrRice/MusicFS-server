package plugins_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestNCMArtworkLifecycle(t *testing.T) {
	worker, hash := buildNCMWorker(t)
	for _, mode := range []struct {
		name   string
		config ncmdecoder.Config
	}{
		{"builtin", ncmdecoder.Config{Builtin: true, MaxConcurrent: 1}},
		{"process", ncmdecoder.Config{WorkerPath: worker, WorkerSHA256: hash, MaxConcurrent: 1}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			processor, err := ncmdecoder.NewProcessor(mode.config)
			if err != nil {
				t.Fatal(err)
			}
			plain := append([]byte{0xff, 0xfb, 0x90, 0}, bytes.Repeat([]byte{0x55}, 8192)...)
			cover := bytes.Repeat([]byte("opaque artwork"), 10000)
			for _, cancelTask := range []bool{false, true} {
				encrypted, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{Cover: cover, CoverPadding: 1 << 20})
				if err != nil {
					t.Fatal(err)
				}
				input := &countedNCMInput{Reader: bytes.NewReader(encrypted)}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				decoded, err := processor.Open(ctx, "artwork", "asset", input, int64(len(encrypted)))
				if err != nil {
					t.Fatal(err)
				}
				defer decoded.Close()
				if input.bytes.Load() > 4096 {
					t.Fatalf("artwork was read eagerly: %d", input.bytes.Load())
				}
				artwork, size := decoded.Artwork()
				if artwork == nil || size != int64(len(cover)) {
					t.Fatalf("invalid artwork descriptor: %d", size)
				}
				got := make([]byte, len(cover)+1)
				if n, err := artwork.ReadAt(got, 0); n != len(cover) || !errors.Is(err, io.EOF) || !bytes.Equal(got[:n], cover) {
					t.Fatalf("artwork range mismatch: %d %v", n, err)
				}
				if input.max.Load() > 65536 {
					t.Fatalf("unbounded artwork source read: %d", input.max.Load())
				}
				audio, err := io.ReadAll(io.NewSectionReader(decoded, 0, decoded.Size()))
				if err != nil || !bytes.Equal(audio, plain) {
					t.Fatalf("artwork changed decrypted audio: %v", err)
				}
				if cancelTask {
					cancel()
					if _, err := artwork.ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel did not revoke artwork: %v", err)
					}
				}
				if err := decoded.Close(); err != nil {
					t.Fatal(err)
				}
				if _, err := artwork.ReadAt(make([]byte, 1), 0); err == nil {
					t.Fatal("closed artwork remains readable")
				}
			}
			for _, padding := range []int{0, 2048} {
				encrypted, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{CoverPadding: padding})
				if err != nil {
					t.Fatal(err)
				}
				decoded, err := processor.Open(context.Background(), "empty", "asset", bytes.NewReader(encrypted), int64(len(encrypted)))
				if err != nil {
					t.Fatal(err)
				}
				if artwork, size := decoded.Artwork(); artwork != nil || size != 0 {
					t.Fatalf("empty artwork was exposed: %d", size)
				}
				if err := decoded.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestNCMAudioDecoderExposesArtworkAttachment(t *testing.T) {
	processor, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	cover := []byte("container artwork")
	plain := append([]byte("fLaC"), bytes.Repeat([]byte{0}, 128)...)
	encrypted, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{Cover: cover, CoverPadding: 64})
	if err != nil {
		t.Fatal(err)
	}
	output, err := processor.AudioDecoder().Decode(context.Background(), audioformat.Request{TaskID: "attachment", AssetID: "asset"}, audioformat.Input{Reader: bytes.NewReader(encrypted), Size: int64(len(encrypted))})
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if len(output.Attachments) != 1 || output.Attachments[0].Kind != "artwork" || output.Attachments[0].Size != int64(len(cover)) {
		t.Fatalf("invalid audio decoder attachments: %+v", output.Attachments)
	}
	attachment := output.Attachments[0]
	got, err := io.ReadAll(io.NewSectionReader(attachment.Reader, 0, attachment.Size))
	if err != nil || !bytes.Equal(got, cover) {
		t.Fatalf("attachment bytes changed: %v", err)
	}
}
