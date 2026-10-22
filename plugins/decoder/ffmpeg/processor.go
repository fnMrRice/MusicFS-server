package audioffmpeg

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
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

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/builtin"
	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio/dspworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type Config struct {
	Builtin                                                    bool
	InstanceID                                                 string
	WorkerArgs                                                 []string
	FFmpegPath, FFprobePath, WorkerPath, WorkerSHA256, WorkDir string
	MaxConcurrent                                              int
	DSPChain                                                   []audio.NodeSpec
	DSPPlugins                                                 []dspworker.Config
	OutputLayout                                               string
	BufferFrames                                               int
	DisableEffects                                             bool
}
type Processor struct {
	parent        *Processor
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
	if cfg.DisableEffects {
		cfg.DSPChain, cfg.DSPPlugins = nil, nil
	}
	if _, err := bufferFrames(cfg.BufferFrames); err != nil {
		return nil, err
	}
	cfg.WorkerArgs = slices.Clone(cfg.WorkerArgs)
	cfg.DSPChain = CloneDSPChain(cfg.DSPChain)
	cfg.DSPPlugins = slices.Clone(cfg.DSPPlugins)
	for i := range cfg.DSPPlugins {
		cfg.DSPPlugins[i] = cfg.DSPPlugins[i].Clone()
	}
	if err := validateExecution(cfg); err != nil {
		return nil, err
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
	hash, err := workerHash(cfg)
	if err != nil {
		return nil, err
	}
	if !cfg.Builtin && (cfg.WorkerSHA256 == "" || hash != cfg.WorkerSHA256) {
		return nil, fmt.Errorf("ACCESS_DENIED: audio worker checksum mismatch")
	}
	dspVersions := map[string]string{}
	format, err := audio.FormatForLayout(cfg.OutputLayout)
	if err != nil || format.Layout == "mono" {
		return nil, fmt.Errorf("UNSUPPORTED: output layout must be stereo, 5.1 or 7.1")
	}
	var chain *audio.Chain
	if cfg.DisableEffects {
		chain, err = buildRequestedDSP(ctx, StartRequest{DisableEffects: true}, format)
	} else {
		chain, err = buildDSPFormatVersions(ctx, 0, cfg.DSPChain, cfg.DSPPlugins, format, dspVersions)
	}
	if err != nil {
		return nil, err
	}
	if chain.Output().Format != format {
		chain.Close()
		return nil, fmt.Errorf("UNSUPPORTED: DSP output differs from selected encoding layout")
	}
	chain.Close()
	capabilities, err := Probe(ctx, cfg.FFmpegPath, cfg.FFprobePath)
	if err != nil {
		return nil, err
	}
	processor := &Processor{config: cfg, dspVersions: dspVersions, Capabilities: capabilities, slots: make(chan struct{}, cfg.MaxConcurrent), normalizeSlot: make(chan struct{}, 1), identity: ProcessingIdentity{WorkerSHA256: hash, FFmpegSHA256: capabilities.FFmpegSHA256, PluginVersion: audioPluginVersion, PipelineRevision: dspRevision(cfg, hash)}}
	if _, err := processor.OutputProfile(); err != nil {
		return nil, err
	}
	if err := processor.ValidateSettings(ctx, processor.Defaults()); err != nil {
		return nil, err
	}
	return processor, nil
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
	profile, profileErr := p.OutputProfile()
	if profileErr != nil || profile.ID != plan.Profile {
		return fmt.Errorf("UNSUPPORTED: audio plan differs from configured output")
	}
	if err := p.SupportsDecodeOutput(plan.DecodeOutput); err != nil {
		return err
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
		request := StartRequest{Plan: plan, FFmpegPath: p.config.FFmpegPath, FFmpegSHA256: p.Capabilities.FFmpegSHA256, InputURL: inputURL, OutputURL: outputURL, DSPChain: p.config.DSPChain, DSPPlugins: p.config.DSPPlugins, BufferFrames: p.config.BufferFrames, DisableEffects: p.config.DisableEffects}
		if err := peer.Call(ctx, "audio.prepare", request, nil); err != nil {
			return err
		}
		return peer.Call(ctx, "audio.start", request, nil)
	})
}

func (p *Processor) run(ctx context.Context, taskID string, readerAt io.ReaderAt, size int64, output io.Writer, operation func(*pluginproto.Peer, string, string) error) (runErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.decoder.ffmpeg", p.config.InstanceID)
	finish := pluginlog.Operation(ctx, "audio.process")
	defer func() { finish(runErr) }()
	pluginlog.Logger(ctx).Log(ctx, pluginlog.LevelTrace, "Audio task admitted", "inputBytes", size, "builtin", p.config.Builtin, "dspNodes", len(p.config.DSPChain))
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := validateExecution(p.config); err != nil {
		return err
	}
	if readerAt == nil || output == nil || size <= 0 {
		return fmt.Errorf("ACCESS_DENIED: invalid audio task input")
	}
	hash, err := workerHash(p.config)
	if err != nil || !p.config.Builtin && hash != p.config.WorkerSHA256 {
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
	var handlers inprocess.Handlers
	server := &http.Server{Handler: handlers.Wrap(mux), ReadHeaderTimeout: 3 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	go server.Serve(listener)
	defer func() { server.Close(); handlers.Wait() }()
	var peer *pluginproto.Peer
	if p.config.Builtin {
		local := inprocess.Start(ctx, RunWorker)
		peer = pluginproto.NewPeer(ctx, local.Output, local.Input, "h-", nil)
		defer func() { peer.Close(); local.Close(); <-local.Done }()
	} else {
		cmd := exec.CommandContext(ctx, p.config.WorkerPath, p.config.WorkerArgs...)
		configureProcess(cmd)
		cmd.Dir = p.config.WorkDir
		stderr := logging.PluginStderr("musicfs.plugin.decoder.ffmpeg", p.config.InstanceID)
		cmd.Stderr = stderr
		defer func() {
			if cmd.Process == nil {
				_ = stderr.Close()
			}
		}()
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
			stderr.Close()
			return fmt.Errorf("UNSUPPORTED: process cleanup containment unavailable")
		}
		defer cleanupTree()
		peer = pluginproto.NewPeer(ctx, stdout, stdin, "h-", nil)
		done := make(chan struct{})
		go func() { cmd.Wait(); stderr.Close(); peer.Close(); close(done) }()
		defer func() {
			peer.Close()
			select {
			case <-done:
			case <-time.After(time.Second):
				cmd.Process.Kill()
				<-done
			}
		}()
	}
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
	for _, dsp := range p.config.DSPPlugins {
		if dsp.Builtin && !slices.Contains(ready.Manifest.Capabilities, "audio.dsp.builtin") {
			return fmt.Errorf("PROTOCOL_MISMATCH: audio worker does not support builtin DSP")
		}
	}
	version := ready.Manifest.PluginVersion
	p.runtime().version.Store(&version)
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

func workerHash(cfg Config) (string, error) {
	if cfg.Builtin {
		hash := sha256.Sum256([]byte("musicfs.plugin.decoder.ffmpeg:builtin:" + audioPluginVersion))
		return hex.EncodeToString(hash[:]), nil
	}
	return executableHash(cfg.WorkerPath)
}

// 内置 DSP 必须在 Server 主进程执行，不能随外部音频 Worker 转移到子进程。
func validateExecution(cfg Config) error {
	if !cfg.Builtin {
		if cfg.OutputLayout != "" && cfg.OutputLayout != "stereo" {
			return fmt.Errorf("UNSUPPORTED: channel conversion requires builtin audio execution")
		}
		for _, node := range cfg.DSPChain {
			if builtin.EffectName(node.Name) != "" {
				return fmt.Errorf("UNSUPPORTED: builtin DSP requires builtin audio execution")
			}
		}
		for _, plugin := range cfg.DSPPlugins {
			if plugin.Builtin {
				return fmt.Errorf("UNSUPPORTED: builtin DSP requires builtin audio execution")
			}
		}
	}
	return nil
}

// PluginVersion returns only a release verified by a completed worker handshake.
func (p *Processor) PluginVersion() string {
	if version := p.runtime().version.Load(); version != nil {
		return *version
	}
	return ""
}
