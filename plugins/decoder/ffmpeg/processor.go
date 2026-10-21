package audioffmpeg

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"slices"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type Config struct {
	InstanceID                                                 string
	WorkerArgs                                                 []string
	FFmpegPath, FFprobePath, WorkerPath, WorkerSHA256, WorkDir string
	MaxConcurrent                                              int
	DSPChain                                                   []audio.NodeSpec
	DSPPlugins                                                 []dspworker.Config
}
type Processor struct {
	gate          atomic.Pointer[lifecycleBinding]
	version       atomic.Pointer[string]
	dspVersions   map[string]string
	config        Config
	Capabilities  Capabilities
	slots         chan struct{}
	normalizeSlot chan struct{}
	identity      ProcessingIdentity
}

func NewProcessor(ctx context.Context, cfg Config) (*Processor, error) {
	cfg.WorkerArgs = slices.Clone(cfg.WorkerArgs)
	cfg.DSPChain = CloneDSPChain(cfg.DSPChain)
	cfg.DSPPlugins = slices.Clone(cfg.DSPPlugins)
	for i := range cfg.DSPPlugins {
		cfg.DSPPlugins[i] = cfg.DSPPlugins[i].Clone()
	}
	if err := ValidateDSP(cfg.DSPChain, cfg.DSPPlugins); err != nil {
		return nil, err
	}
	if cfg.InstanceID == "" {
		cfg.InstanceID = uuid.NewString()
	}
	if !instanceid.Valid(cfg.InstanceID) {
		return nil, fmt.Errorf("invalid plugin instance UUID")
	}
	if cfg.MaxConcurrent == 0 {
		cfg.MaxConcurrent = 2
	}
	if cfg.MaxConcurrent < 1 || cfg.MaxConcurrent > 8 {
		return nil, fmt.Errorf("invalid audio concurrency limit")
	}
	hash, err := executableHash(cfg.WorkerPath)
	if err != nil {
		return nil, err
	}
	if cfg.WorkerSHA256 == "" || hash != cfg.WorkerSHA256 {
		return nil, fmt.Errorf("ACCESS_DENIED: audio worker checksum mismatch")
	}
	dspVersions := map[string]string{}
	chain, err := buildDSPWithVersions(ctx, 0, cfg.DSPChain, cfg.DSPPlugins, dspVersions)
	if err != nil {
		return nil, err
	}
	chain.Close()
	capabilities, err := Probe(ctx, cfg.FFmpegPath, cfg.FFprobePath)
	if err != nil {
		return nil, err
	}
	return &Processor{config: cfg, dspVersions: dspVersions, Capabilities: capabilities, slots: make(chan struct{}, cfg.MaxConcurrent), normalizeSlot: make(chan struct{}, 1), identity: ProcessingIdentity{WorkerSHA256: hash, FFmpegSHA256: capabilities.FFmpegSHA256, PluginVersion: audioPluginVersion, PipelineRevision: dspRevision(cfg, hash)}}, nil
}

type bootstrap struct {
	pluginproto.Hello
	Capabilities Capabilities `json:"capabilities"`
}

func (p *Processor) Process(ctx context.Context, plan Plan, input io.ReadSeeker, size int64, output io.Writer) error {
	lease, err := p.AcquireLease(ctx)
	if err != nil {
		return err
	}
	defer lease.Close()
	if lease != nil {
		ctx = lease.Context()
	}
	if plan.TaskID == "" || plan.AssetID == "" || size <= 0 {
		return fmt.Errorf("ACCESS_DENIED: invalid task input")
	}
	readerAt, ok := input.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("UNSUPPORTED: independent random input readers required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case p.slots <- struct{}{}:
		defer func() { <-p.slots }()
	default:
		return fmt.Errorf("RATE_LIMITED: audio processing is busy")
	}
	return p.run(ctx, plan.TaskID, readerAt, size, output, func(peer *pluginproto.Peer, inputURL, outputURL string) error {
		request := StartRequest{Plan: plan, FFmpegPath: p.config.FFmpegPath, FFmpegSHA256: p.Capabilities.FFmpegSHA256, InputURL: inputURL, OutputURL: outputURL, DSPChain: p.config.DSPChain, DSPPlugins: p.config.DSPPlugins}
		if err := peer.Call(ctx, "audio.prepare", request, nil); err != nil {
			return err
		}
		return peer.Call(ctx, "audio.start", request, nil)
	})
}

func (p *Processor) run(ctx context.Context, taskID string, readerAt io.ReaderAt, size int64, output io.Writer, operation func(*pluginproto.Peer, string, string) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if readerAt == nil || output == nil || size <= 0 {
		return fmt.Errorf("ACCESS_DENIED: invalid audio task input")
	}
	hash, err := executableHash(p.config.WorkerPath)
	if err != nil || hash != p.config.WorkerSHA256 {
		return fmt.Errorf("ACCESS_DENIED: audio worker changed")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	var random [24]byte
	rand.Read(random[:])
	token := hex.EncodeToString(random[:])
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("TEMPORARY_FAILURE: audio channel unavailable")
	}
	base := "http://" + listener.Addr().String() + "/" + token
	var outputStarted atomic.Bool
	outputDone := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/"+token+"/input", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if ctx.Err() != nil {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, "input", time.Time{}, io.NewSectionReader(readerAt, 0, size))
	})
	mux.HandleFunc("/"+token+"/output", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !outputStarted.CompareAndSwap(false, true) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_, copyErr := io.Copy(output, r.Body)
		outputDone <- copyErr
		if copyErr != nil {
			cancel()
			w.WriteHeader(http.StatusBadGateway)
		} else {
			w.WriteHeader(http.StatusNoContent)
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	go server.Serve(listener)
	defer server.Close()
	cmd := exec.CommandContext(ctx, p.config.WorkerPath, p.config.WorkerArgs...)
	configureProcess(cmd)
	cmd.Dir = p.config.WorkDir
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err = cmd.Start(); err != nil {
		return fmt.Errorf("UNSUPPORTED: audio worker failed to start")
	}
	cleanupTree, err := containProcess(cmd.Process.Pid)
	if err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return fmt.Errorf("UNSUPPORTED: process cleanup containment unavailable")
	}
	defer cleanupTree()
	peer := pluginproto.NewPeer(ctx, stdout, stdin, "h-", nil)
	done := make(chan struct{})
	go func() { cmd.Wait(); peer.Close(); close(done) }()
	defer func() {
		peer.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			cmd.Process.Kill()
			<-done
		}
	}()
	startup, startupCancel := context.WithTimeout(ctx, 5*time.Second)
	defer startupCancel()
	var ready pluginproto.Ready
	err = peer.Call(startup, "host.hello", bootstrap{Hello: pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: p.config.InstanceID, TaskID: taskID, Nonce: token, ConfigRevision: "1"}, Capabilities: p.Capabilities}, &ready)
	if err != nil {
		return err
	}
	if err = pluginconfig.ValidateManifest(ready.Manifest, "audio"); err != nil {
		return err
	}
	if ready.Nonce != token || ready.InstanceID != p.config.InstanceID {
		return fmt.Errorf("PROTOCOL_MISMATCH: audio worker identity mismatch")
	}
	if ready.Manifest.PluginID != "musicfs.plugin.decoder.ffmpeg" || ready.Manifest.PluginVersion != audioPluginVersion {
		return fmt.Errorf("PROTOCOL_MISMATCH: audio worker implementation differs")
	}
	version := ready.Manifest.PluginVersion
	p.version.Store(&version)
	if err = operation(peer, base+"/input", base+"/output"); err != nil {
		return err
	}
	select {
	case err = <-outputDone:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// PluginVersion returns only a release verified by a completed worker handshake.
func (p *Processor) PluginVersion() string {
	if version := p.version.Load(); version != nil {
		return *version
	}
	return ""
}
