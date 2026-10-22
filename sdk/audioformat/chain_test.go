package audioformat_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audioformat"
)

func chainInput(content string) audioformat.Input {
	return audioformat.Input{Reader: strings.NewReader(content), Size: int64(len(content)), Format: "test", MIME: "audio/test"}
}

func chainDecoder(id string, priority int, prefix string, decode func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error)) audioformat.Decoder {
	d := detector(id, priority)
	d.Probe = func(header []byte, _ int64) (audioformat.Match, bool) {
		return audioformat.Match{Format: "test", MIME: "audio/test"}, strings.HasPrefix(string(header), prefix)
	}
	return audioformat.Decoder{Detector: d, Decode: decode}
}

func TestChainReprobesEachLayerAndClosesResourcesInReverse(t *testing.T) {
	var called, closed []string
	stage := func(id, next string, more bool) func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
		return func(_ context.Context, request audioformat.Request, _ audioformat.Input) (audioformat.Output, error) {
			if request.OutputFormat != "pcm_f32le" || request.MaxOutputBytes != 1234 {
				t.Fatalf("request lost: %+v", request)
			}
			called = append(called, id)
			return audioformat.Output{Input: chainInput(next), NeedsDecoding: more, Close: func() error { closed = append(closed, id); return nil }}, nil
		}
	}
	chain, err := audioformat.NewChain(
		chainDecoder("inner", 20, "inner", stage("inner", "PCM", false)),
		chainDecoder("outer", 10, "outer", stage("outer", "inner", true)),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := chain.Decode(context.Background(), audioformat.Request{OutputFormat: "pcm_f32le", MaxOutputBytes: 1234}, chainInput("outer"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(called, []string{"outer", "inner"}) || !reflect.DeepEqual(result.Layers, called) || result.Size != 3 {
		t.Fatalf("wrong chain: %+v %+v", called, result)
	}
	if err := result.Close(); err != nil {
		t.Fatal(err)
	}
	result.Close()
	if !reflect.DeepEqual(closed, []string{"inner", "outer"}) {
		t.Fatalf("wrong close order: %v", closed)
	}
}

func TestChainFallsBackOnlyForExplicitUnsupported(t *testing.T) {
	for _, test := range []struct {
		name        string
		firstError  error
		wantSuccess bool
	}{
		{"unsupported", fmt.Errorf("codec: %w", audioformat.ErrUnsupported), true},
		{"damaged input", errors.New("UNSUPPORTED: corrupt encrypted container"), false},
		{"disabled", errors.New("PLUGIN_DISABLED: decoder unavailable"), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			attemptClosed, fallbackCalled := 0, false
			chain, err := audioformat.NewChain(
				chainDecoder("preferred", 100, "", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
					return audioformat.Output{Close: func() error { attemptClosed++; return nil }}, test.firstError
				}),
				chainDecoder("fallback", 10, "", func(_ context.Context, _ audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
					fallbackCalled = true
					return audioformat.Output{Input: input}, nil
				}),
			)
			if err != nil {
				t.Fatal(err)
			}
			result, err := chain.Decode(context.Background(), audioformat.Request{}, chainInput("source"))
			if test.wantSuccess != (err == nil) || test.wantSuccess != fallbackCalled || attemptClosed != 1 {
				t.Fatalf("invalid fallback: %v %v %d", err, fallbackCalled, attemptClosed)
			}
			if result != nil {
				result.Close()
			}
		})
	}
}

func TestChainPreservesRegistrationOrderAndExplicitTerminalOutput(t *testing.T) {
	chosen := ""
	makeDecoder := func(id string) audioformat.Decoder {
		return chainDecoder(id, 5, "source", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			chosen = id
			return audioformat.Output{Input: chainInput("terminal bytes")}, nil
		})
	}
	chain, err := audioformat.NewChain(makeDecoder("z-first"), makeDecoder("a-second"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := chain.Decode(context.Background(), audioformat.Request{}, chainInput("source"))
	if err != nil || chosen != "z-first" {
		t.Fatalf("registration order lost: %s %v", chosen, err)
	}
	result.Close()
}

func TestChainRejectsCyclesDepthAndInvalidOutputsWithCleanup(t *testing.T) {
	for _, test := range []struct {
		name           string
		cycle, invalid bool
		wantCalls      int
	}{
		{"repeated bytes", true, false, 1},
		{"depth limit", false, false, audioformat.MaxDecodeDepth},
		{"invalid output", false, true, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls, closed := 0, 0
			chain, err := audioformat.NewChain(chainDecoder("loop", 1, "", func(_ context.Context, _ audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
				calls++
				output := chainInput(fmt.Sprintf("layer-%d", calls))
				if test.cycle {
					output = input
				}
				if test.invalid {
					output.Reader = nil
				}
				return audioformat.Output{Input: output, NeedsDecoding: true, Close: func() error { closed++; return nil }}, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := chain.Decode(context.Background(), audioformat.Request{}, chainInput("original")); err == nil {
				t.Fatal("invalid chain succeeded")
			}
			if calls != test.wantCalls || closed != calls {
				t.Fatalf("resource leak: calls=%d closed=%d", calls, closed)
			}
		})
	}
}

func TestChainCancellationAndUnknownFormat(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	closed := false
	chain, err := audioformat.NewChain(chainDecoder("cancel", 1, "source", func(_ context.Context, _ audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
		cancel()
		return audioformat.Output{Input: input, Close: func() error { closed = true; return nil }}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := chain.Decode(context.Background(), audioformat.Request{}, chainInput("unknown")); !errors.Is(err, audioformat.ErrUnsupported) {
		t.Fatalf("unknown matched: %v", err)
	}
	if _, err := chain.Decode(ctx, audioformat.Request{}, chainInput("source")); !errors.Is(err, context.Canceled) || !closed {
		t.Fatalf("cancel resource leak: %v %v", err, closed)
	}
}

func TestChainRejectsEmptyOrOversizedOutputAndPreservesProbeInput(t *testing.T) {
	for _, size := range []int64{0, 21} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			closed := false
			first := chainDecoder("first", 10, "", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
				return audioformat.Output{}, errors.New("unexpected decoder call")
			})
			first.Probe = func(header []byte, _ int64) (audioformat.Match, bool) {
				header[0] = 'X'
				return audioformat.Match{}, false
			}
			second := chainDecoder("second", 5, "original", func(_ context.Context, _ audioformat.Request, input audioformat.Input) (audioformat.Output, error) {
				input.Size = size
				return audioformat.Output{Input: input, Close: func() error { closed = true; return nil }}, nil
			})
			chain, err := audioformat.NewChain(first, second)
			if err != nil {
				t.Fatal(err)
			}
			_, err = chain.Decode(context.Background(), audioformat.Request{MaxOutputBytes: 20}, chainInput("original"))
			if err == nil || !closed {
				t.Fatalf("output limit or isolation failed: %v %v", err, closed)
			}
		})
	}
}

func TestDecodedCloseRetriesOnlyFailedResources(t *testing.T) {
	var closed []string
	innerAttempts := 0
	cleanupFailure := errors.New("output is still locked")
	chain, err := audioformat.NewChain(
		chainDecoder("outer", 10, "outer", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			return audioformat.Output{Input: chainInput("inner"), NeedsDecoding: true, Close: func() error {
				closed = append(closed, "outer")
				return nil
			}}, nil
		}),
		chainDecoder("inner", 10, "inner", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			return audioformat.Output{Input: chainInput("PCM"), Close: func() error {
				closed = append(closed, "inner")
				innerAttempts++
				if innerAttempts == 1 {
					return cleanupFailure
				}
				return nil
			}}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	result, err := chain.Decode(context.Background(), audioformat.Request{}, chainInput("outer"))
	if err != nil {
		t.Fatal(err)
	}
	if err = result.Close(); !errors.Is(err, audioformat.ErrCleanup) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("cleanup failure was lost: %v", err)
	}
	if !reflect.DeepEqual(closed, []string{"inner", "outer"}) {
		t.Fatalf("wrong first close order: %v", closed)
	}
	if err = result.Close(); err != nil {
		t.Fatalf("successful retry kept a stale error: %v", err)
	}
	result.Close()
	if !reflect.DeepEqual(closed, []string{"inner", "outer", "inner"}) {
		t.Fatalf("successful resources were closed again: %v", closed)
	}
}

func TestChainFailureReportsUnreleasedResource(t *testing.T) {
	cleanupFailure := errors.New("file deletion failed")
	for _, decodeFailure := range []bool{false, true} {
		chain, err := audioformat.NewChain(chainDecoder("failed", 10, "", func(context.Context, audioformat.Request, audioformat.Input) (audioformat.Output, error) {
			output := audioformat.Output{Input: chainInput(""), Close: func() error { return cleanupFailure }}
			if decodeFailure {
				return output, audioformat.ErrUnsupported
			}
			return output, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = chain.Decode(context.Background(), audioformat.Request{}, chainInput("input")); !errors.Is(err, audioformat.ErrCleanup) || !errors.Is(err, cleanupFailure) {
			t.Fatalf("cleanup failure lost: %v", err)
		}
	}
}
