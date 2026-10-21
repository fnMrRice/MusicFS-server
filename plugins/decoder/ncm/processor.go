package ncmdecoder

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type Config struct {
	InstanceID                        string
	WorkerArgs                        []string
	WorkerPath, WorkerSHA256, WorkDir string
	MaxConcurrent                     int
}

type Processor struct {
	gate    atomic.Pointer[lifecycleBinding]
	version atomic.Pointer[string]
	config  Config
	slots   chan struct{}
}

type Decoded struct {
	reader   *channelReader
	size     int64
	metadata Metadata
	cancel   context.CancelFunc
	closed   <-chan struct{}
}

func NewProcessor(cfg Config) (*Processor, error) {
	cfg.WorkerArgs = slices.Clone(cfg.WorkerArgs)
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
		return nil, fmt.Errorf("invalid NCM concurrency limit")
	}
	if cfg.WorkDir != "" {
		info, err := os.Stat(cfg.WorkDir)
		if !filepath.IsAbs(cfg.WorkDir) || err != nil || !info.IsDir() {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid NCM worker directory")
		}
	}
	if err := verifyWorker(cfg); err != nil {
		return nil, err
	}
	return &Processor{config: cfg, slots: make(chan struct{}, cfg.MaxConcurrent)}, nil
}

func (p *Processor) Open(ctx context.Context, taskID, assetID string, input io.ReaderAt, size int64) (*Decoded, error) {
	lease, err := p.AcquireLease(ctx)
	if err != nil {
		return nil, err
	}
	leaseTransferred := false
	defer func() {
		if !leaseTransferred {
			lease.Close()
		}
	}()
	if lease != nil {
		ctx = lease.Context()
	}
	if !validID(taskID) || len(taskID) > 196 || !validID(assetID) || input == nil || size <= 0 {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid NCM task input")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case p.slots <- struct{}{}:
	default:
		return nil, fmt.Errorf("RATE_LIMITED: NCM decoding is busy")
	}
	release := true
	defer func() {
		if release {
			<-p.slots
		}
	}()
	if err := verifyWorker(p.config); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer func() {
		if release {
			cancel()
		}
	}()
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("TEMPORARY_FAILURE: NCM channel initialization failed")
	}
	nonce := hex.EncodeToString(random[:])
	channel, err := newDataChannel(ctx, nonce, "input", input, size)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, p.config.WorkerPath, p.config.WorkerArgs...)
	cmd.Dir, cmd.Stderr, cmd.WaitDelay = p.config.WorkDir, io.Discard, time.Second
	cmd.Env = []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if value := os.Getenv(key); value != "" {
			cmd.Env = append(cmd.Env, key+"="+value)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		channel.Close()
		return nil, fmt.Errorf("TEMPORARY_FAILURE: NCM control channel unavailable")
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		channel.Close()
		return nil, fmt.Errorf("TEMPORARY_FAILURE: NCM control channel unavailable")
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		channel.Close()
		return nil, fmt.Errorf("UNSUPPORTED: NCM worker failed to start")
	}
	peer := pluginproto.NewPeer(ctx, stdout, stdin, "h-", nil)
	processDone := make(chan struct{})
	go func() { _ = cmd.Wait(); cancel(); _ = peer.Close(); close(processDone) }()
	cleaned := make(chan struct{})
	var remote *channelReader
	// Cleanup ownership moves to this goroutine before any fallible RPC calls.
	release = false
	leaseTransferred = true
	readyToClean := make(chan struct{})
	go func() {
		defer lease.Close()
		<-ctx.Done()
		_ = peer.Close()
		channel.Close()
		<-readyToClean
		if remote != nil {
			remote.Close()
		}
		<-processDone
		<-p.slots
		close(cleaned)
	}()
	success := false
	defer func() {
		close(readyToClean)
		if !success {
			cancel()
			<-cleaned
		}
	}()
	startup, stopStartup := context.WithTimeout(ctx, 5*time.Second)
	defer stopStartup()
	var ready pluginproto.Ready
	hello := pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: p.config.InstanceID, TaskID: taskID, Nonce: nonce, ConfigRevision: "1"}
	if err = peer.Call(startup, "host.hello", hello, &ready); err != nil {
		return nil, err
	}
	if pluginconfig.ValidateManifest(ready.Manifest, "audio") != nil || ready.Manifest.PluginID != "musicfs.plugin.decoder.ncm" || !slices.Contains(ready.Manifest.Capabilities, "audio.decrypt") || ready.Nonce != nonce || ready.InstanceID != hello.InstanceID {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: NCM worker identity mismatch")
	}
	version := ready.Manifest.PluginVersion
	p.version.Store(&version)
	var result openResult
	if err = peer.Call(startup, "ncm.open", openRequest{TaskID: taskID, AssetID: assetID, Nonce: nonce, InputURL: channel.url, Size: size}, &result); err != nil {
		return nil, err
	}
	validFormat := result.Metadata.Format == "mp3" && result.Metadata.MIME == "audio/mpeg" || result.Metadata.Format == "flac" && result.Metadata.MIME == "audio/flac"
	if result.TaskID != taskID || result.AssetID != assetID || result.Nonce != nonce || result.Size <= 0 || result.Size >= size || !validFormat {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid NCM output descriptor")
	}
	remote, err = newChannelReader(ctx, result.OutputURL, nonce, "output", result.Size)
	if err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	success = true
	return &Decoded{reader: remote, size: result.Size, metadata: cloneMetadata(result.Metadata), cancel: cancel, closed: cleaned}, nil
}

// PluginVersion returns only a release verified by a completed worker handshake.
func (p *Processor) PluginVersion() string {
	if version := p.version.Load(); version != nil {
		return *version
	}
	return ""
}

func (d *Decoded) ReadAt(dst []byte, offset int64) (int, error) { return d.reader.ReadAt(dst, offset) }
func (d *Decoded) Size() int64                                  { return d.size }
func (d *Decoded) Metadata() Metadata                           { return cloneMetadata(d.metadata) }
func (d *Decoded) Close() error {
	d.cancel()
	<-d.closed
	return nil
}

func verifyWorker(cfg Config) error {
	if !filepath.IsAbs(cfg.WorkerPath) || len(cfg.WorkerSHA256) != 64 {
		return fmt.Errorf("ACCESS_DENIED: invalid NCM worker configuration")
	}
	f, err := os.Open(cfg.WorkerPath)
	if err != nil {
		return fmt.Errorf("ACCESS_DENIED: NCM worker unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("ACCESS_DENIED: invalid NCM worker file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil || hex.EncodeToString(hash.Sum(nil)) != cfg.WorkerSHA256 {
		return fmt.Errorf("ACCESS_DENIED: NCM worker checksum mismatch")
	}
	return nil
}
