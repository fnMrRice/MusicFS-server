package builtinruntime_test

import (
	"bytes"
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	scraperlocal "github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

const instanceID = "d248bf34-1780-59d3-a557-9cf5d889e977"

func TestBuiltinProvidersStartWithoutExecutablesAndCloseIndependently(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first, err := pluginhost.Start(ctx, pluginhost.Config{Builtin: true, StateDir: t.TempDir(), UserID: "owner", InstanceID: instanceID,
		Roots: []sourceaccess.RootConfig{{ID: "first", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { first.Close(); cancel() })
	second, err := pluginhost.Start(t.Context(), pluginhost.Config{Builtin: true, StateDir: t.TempDir(), UserID: "other-owner", InstanceID: "16b003e5-1bab-52bb-be10-0e83ec1c40ea",
		Roots: []sourceaccess.RootConfig{{ID: "second", Path: t.TempDir()}, {ID: "third", Path: t.TempDir()}}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if first.DisplayCount() == nil || first.DisplayCount().Value != 1 || second.DisplayCount() == nil || second.DisplayCount().Value != 2 {
		t.Fatal("builtin provider instances shared startup state")
	}
	cancel()
	if err = first.Close(); err != nil || first.DisplayCount() != nil {
		t.Fatal("canceled builtin provider remained active", err)
	}
	if second.DisplayCount() == nil || second.DisplayCount().Value != 2 {
		t.Fatal("closing one builtin provider affected another instance")
	}
}

func TestBuiltinOnlinePlatformsStartWithoutExecutables(t *testing.T) {
	for _, platform := range []string{online.Netease, online.QQMusic, online.AppleMusic} {
		t.Run(platform, func(t *testing.T) {
			w, err := pluginhost.StartOnline(t.Context(), pluginhost.OnlineConfig{Builtin: true, StateDir: t.TempDir(), UserID: "owner", InstanceID: instanceID, Platform: platform})
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			view, err := w.PlatformSession(t.Context())
			if err != nil || view.State != "signedOut" || w.PluginIdentity().ID != pluginproto.OnlinePluginID(platform) {
				t.Fatalf("builtin online account was not scoped correctly: %+v %v", view, err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuiltinNCMDecodesAndReclaimsCanceledSlot(t *testing.T) {
	plain, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := testkit.BuildNCM(plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		decoded, err := processor.Open(ctx, "decode", "asset", bytes.NewReader(encrypted), int64(len(encrypted)))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		got := make([]byte, len(plain))
		n, err := decoded.ReadAt(got, 0)
		if err != nil || n != len(plain) || !bytes.Equal(got, plain) {
			t.Fatal("builtin decoder produced different audio", err)
		}
		cancel()
		if err := decoded.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := decoded.ReadAt(got[:4], 0); err == nil {
			t.Fatal("canceled builtin decoder remained readable")
		}
	}
	if processor.PluginVersion() != bundledplugin.NCMVersion {
		t.Fatal("builtin decoder did not verify its declaration")
	}
}

func TestBuiltinScraperKeepsSessionCandidatesIsolated(t *testing.T) {
	host, err := scraperlocal.NewHost(scraperlocal.Config{Builtin: true, InstanceID: instanceID, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("[00:01]Builtin lyrics")
	input := scraperlocal.Input{Resource: enrichment.Resource{ResourceRef: strings.Repeat("a", 48), Version: "v1", Kind: "lyrics", SizeBytes: int64(len(body)), Match: "exactStem"}, Reader: bytes.NewReader(body), Validate: func() error { return nil }}
	var previous string
	for range 2 {
		ctx, cancel := context.WithCancel(t.Context())
		session, err := host.Open(ctx, pluginproto.Context{UserID: "owner", InstanceID: instanceID, TaskID: "search", TimeoutMs: 30000}, enrichment.Target{SourceTrackID: "source", AssetID: "asset", AssetVersion: "v1"}, []scraperlocal.Input{input})
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		result, err := session.Search(ctx, []string{"lyrics"}, nil, nil)
		if err != nil || len(result.Candidates) != 1 {
			t.Fatalf("builtin scraper search failed: %+v %v", result, err)
		}
		if previous != "" {
			if _, err := session.Fetch(ctx, previous, io.Discard); err == nil {
				t.Fatal("candidate crossed builtin scraper sessions")
			}
		}
		previous = result.Candidates[0].CandidateID
		var output bytes.Buffer
		if _, err := session.Fetch(ctx, previous, &output); err != nil || !bytes.Equal(output.Bytes(), body) {
			t.Fatal("builtin scraper fetch failed", err)
		}
		cancel()
		if err := session.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBuiltinDSPProcessesAndCancelsWithoutExecutable(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	p, err := dspworker.New(ctx, dspworker.Config{Builtin: true, Name: "musicfs.dsp.gain"}, map[string]float64{"gainDB": -6})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	block := audio.Block{Format: audio.Canonical, Samples: []float32{1, -1}, Frames: 1, Sequence: 7, PositionFrames: 100}
	if err := p.Process(&block); err != nil || math.Abs(float64(block.Samples[0])-math.Pow(10, -6.0/20)) > 0.000001 || block.Sequence != 7 || block.PositionFrames != 100 {
		t.Fatal("builtin DSP changed timing or produced incorrect PCM", err)
	}
	cancel()
	closed := make(chan struct{})
	go func() { p.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("builtin DSP cancellation left blocked protocol IO")
	}
	if err := p.Process(&block); err == nil {
		t.Fatal("canceled builtin DSP accepted a new block")
	}
	if _, err := dspworker.New(t.Context(), dspworker.Config{Builtin: true, Name: "unknown.plugin"}, nil); err == nil {
		t.Fatal("unknown plugin was admitted into the main process")
	}
}
