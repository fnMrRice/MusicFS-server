package audiocodecs_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audiocodecs"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

// backgroundDecoder 只暴露租约入口，Decode 自身不管理生命周期，验证保护由宿主实施。
type backgroundDecoder struct {
	gate   *plugincontrol.Gate
	calls  atomic.Int32
	decode func(context.Context) (audioformat.Output, error)
}

func (d *backgroundDecoder) SetGate(g *plugincontrol.Gate) { d.gate = g }
func (*backgroundDecoder) PluginVersion() string           { return "1.0.0" }
func (d *backgroundDecoder) AcquireLease(ctx context.Context) (*plugincontrol.Lease, error) {
	return d.gate.Acquire(ctx)
}
func (d *backgroundDecoder) AudioDecoder() audioformat.Decoder {
	return audioformat.Decoder{
		Detector: audioformat.Detector{PluginID: "example.decoder.background", Priority: 1000, Probe: func(header []byte, _ int64) (audioformat.Match, bool) {
			return audioformat.Match{Format: "fixture", MIME: "audio/x-fixture", RequiresDecode: true}, bytes.HasPrefix(header, []byte("BG01"))
		}},
		Decode: func(ctx context.Context, _ audioformat.Request, _ audioformat.Input) (audioformat.Output, error) {
			d.calls.Add(1)
			return d.decode(ctx)
		},
	}
}

func nativeBackground(t *testing.T, d *backgroundDecoder) (*audioformat.Decoded, error) {
	t.Helper()
	input := []byte("BG01audio")
	return audiocodecs.OpenNative(t.Context(), d, "background-task", "background-asset", bytes.NewReader(input), int64(len(input)))
}

func backgroundOutput(reader io.ReaderAt) audioformat.Output {
	return audioformat.Output{
		Input:       audioformat.Input{Reader: reader, Size: 6, Format: "plain", MIME: "audio/x-plain"},
		Attachments: []audioformat.Attachment{{Kind: "artwork", Reader: bytes.NewReader([]byte("cover")), Size: 5}},
	}
}

func TestBackgroundDecoderAdmissionOwnsReadAndAttachmentLifetime(t *testing.T) {
	gate := plugincontrol.NewGate(false)
	d := &backgroundDecoder{gate: gate, decode: func(context.Context) (audioformat.Output, error) {
		return backgroundOutput(bytes.NewReader([]byte("native"))), nil
	}}
	if output, err := nativeBackground(t, d); err == nil || output != nil || d.calls.Load() != 0 || gate.Active() != 0 {
		t.Fatal("disabled decoder entered background work", err, d.calls.Load(), gate.Active())
	}
	gate.SetAccepting(true, false)
	output, err := nativeBackground(t, d)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if gate.Active() != 1 {
		t.Fatal("background output does not retain its lease", gate.Active())
	}
	gate.SetAccepting(false, false)
	if _, err := output.Reader.ReadAt(make([]byte, 6), 0); err != nil {
		t.Fatal("ordinary drain cancelled admitted output", err)
	}
	if next, err := nativeBackground(t, d); err == nil || next != nil || d.calls.Load() != 1 {
		t.Fatal("draining decoder accepted another task", err)
	}
	gate.SetAccepting(false, true)
	for _, reader := range []io.ReaderAt{output.Reader, output.Attachments[0].Reader} {
		if _, err := reader.ReadAt(make([]byte, 1), 0); !errors.Is(err, context.Canceled) {
			t.Fatal("forced stop did not revoke output or attachment", err)
		}
	}
	if err := output.Close(); err != nil || gate.Active() != 0 {
		t.Fatal("background output retained its lease after close", err, gate.Active())
	}
}

func TestBackgroundDecoderReceivesForcedCancellation(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	entered := make(chan struct{})
	d := &backgroundDecoder{gate: gate, decode: func(ctx context.Context) (audioformat.Output, error) {
		close(entered)
		<-ctx.Done()
		return audioformat.Output{}, ctx.Err()
	}}
	done := make(chan error, 1)
	go func() {
		_, err := nativeBackground(t, d)
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("background decoder did not start")
	}
	gate.SetAccepting(false, true)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || gate.Active() != 0 {
			t.Fatal("cancelled decoder retained work", err, gate.Active())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("decoder did not observe forced cancellation")
	}
}

type backgroundReader func([]byte, int64) (int, error)

func (f backgroundReader) ReadAt(data []byte, offset int64) (int, error) { return f(data, offset) }

func TestBackgroundDecoderRejectsReadCancelledWhileBlocked(t *testing.T) {
	gate := plugincontrol.NewGate(true)
	d := &backgroundDecoder{gate: gate, decode: func(context.Context) (audioformat.Output, error) {
		return backgroundOutput(backgroundReader(func(data []byte, _ int64) (int, error) {
			gate.SetAccepting(false, true)
			return copy(data, []byte("native")), nil
		})), nil
	}}
	output, err := nativeBackground(t, d)
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if _, err := output.Reader.ReadAt(make([]byte, 6), 0); !errors.Is(err, context.Canceled) {
		t.Fatal("read that crossed forced cancellation reported success", err)
	}
}

func TestBackgroundDecoderAdmissionPreservesMissingReaderValidation(t *testing.T) {
	for _, missing := range []string{"audio", "attachment"} {
		t.Run(missing, func(t *testing.T) {
			gate := plugincontrol.NewGate(true)
			d := &backgroundDecoder{gate: gate, decode: func(context.Context) (audioformat.Output, error) {
				output := backgroundOutput(bytes.NewReader([]byte("native")))
				if missing == "audio" {
					output.Reader = nil
				} else {
					output.Attachments[0].Reader = nil
				}
				return output, nil
			}}
			output, err := nativeBackground(t, d)
			if output != nil {
				output.Close()
			}
			if err == nil || output != nil || gate.Active() != 0 {
				t.Fatal("missing decoder reader passed validation or leaked a lease", err, gate.Active())
			}
		})
	}
}
