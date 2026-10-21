package scraperlocal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type Config struct {
	Builtin                                       bool
	WorkerArgs                                    []string
	WorkerPath, WorkerSHA256, WorkDir, InstanceID string
	MaxConcurrent                                 int
}
type Host struct {
	config  Config
	slots   chan struct{}
	version atomic.Pointer[string]
}
type Input struct {
	Resource enrichment.Resource
	Reader   io.ReaderAt
	Validate func() error
}
type Session struct {
	ctx       context.Context
	cancel    context.CancelFunc
	closed    <-chan struct{}
	peer      *pluginproto.Peer
	channel   *hostChannel
	scope     pluginproto.Context
	target    enrichment.Target
	resources []enrichment.Resource
	mu        sync.Mutex
	result    *enrichment.SearchResult
	version   string
}

func NewHost(config Config) (*Host, error) {
	config.WorkerArgs = slices.Clone(config.WorkerArgs)
	if config.MaxConcurrent == 0 {
		config.MaxConcurrent = 2
	}
	if config.MaxConcurrent < 1 || config.MaxConcurrent > 4 || !instanceid.Valid(config.InstanceID) {
		return nil, fmt.Errorf("INVALID_ARGUMENT: invalid scraper host configuration")
	}
	if config.WorkDir != "" {
		info, err := os.Stat(config.WorkDir)
		if !filepath.IsAbs(config.WorkDir) || err != nil || !info.IsDir() {
			return nil, fmt.Errorf("ACCESS_DENIED: invalid scraper working directory")
		}
	}
	if err := verifyWorker(config); err != nil {
		return nil, err
	}
	return &Host{config: config, slots: make(chan struct{}, config.MaxConcurrent)}, nil
}

func (h *Host) Open(parent context.Context, scope pluginproto.Context, target enrichment.Target, inputs []Input) (opened *Session, openErr error) {
	parent = pluginlog.Context(parent, PluginID, h.config.InstanceID)
	finish := pluginlog.Operation(parent, "scraper.open")
	defer func() { finish(openErr) }()
	pluginlog.Logger(parent).Log(parent, pluginlog.LevelTrace, "Scraper inputs admitted", "resourceCount", len(inputs), "builtin", h.config.Builtin)
	if err := parent.Err(); err != nil {
		return nil, err
	}
	resources := make([]enrichment.Resource, len(inputs))
	for i, input := range inputs {
		if input.Reader == nil || input.Validate == nil {
			return nil, fmt.Errorf("ACCESS_DENIED: unguarded scraper input")
		}
		resources[i] = input.Resource
	}
	probe := enrichment.SearchRequest{Context: scope, Target: target, Fields: []string{"lyrics", "artwork"}, Languages: []string{}, Resources: resources}
	if err := probe.Validate(); err != nil {
		return nil, err
	}
	if scope.InstanceID != h.config.InstanceID {
		return nil, fmt.Errorf("ACCESS_DENIED: scraper instance mismatch")
	}
	select {
	case h.slots <- struct{}{}:
	default:
		return nil, fmt.Errorf("RATE_LIMITED: scraper is busy")
	}
	release := true
	defer func() {
		if release {
			<-h.slots
		}
	}()
	if err := verifyWorker(h.config); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(scope.TimeoutMs)*time.Millisecond)
	defer func() {
		if release {
			cancel()
		}
	}()
	nonce, err := randomRef()
	if err != nil {
		return nil, err
	}
	channel, err := newHostChannel(ctx, nonce, inputs)
	if err != nil {
		return nil, err
	}
	failed := true
	defer func() {
		if failed {
			channel.Close()
		}
	}()
	if err = channel.Validate(); err != nil {
		return nil, err
	}
	var peer *pluginproto.Peer
	processDone := make(chan struct{})
	contain := func() {}
	if h.config.Builtin {
		local := inprocess.Start(ctx, RunWorker)
		peer = pluginproto.NewPeer(ctx, local.Output, local.Input, "h-", nil)
		go func() { <-local.Done; cancel(); _ = peer.Close(); close(processDone) }()
	} else {
		cmd := exec.CommandContext(ctx, h.config.WorkerPath, h.config.WorkerArgs...)
		configureProcess(cmd)
		stderr := logging.PluginStderr(PluginID, h.config.InstanceID)
		cmd.Dir, cmd.Stderr, cmd.WaitDelay = h.config.WorkDir, stderr, time.Second
		defer func() {
			if cmd.Process == nil {
				_ = stderr.Close()
			}
		}()
		cmd.Env = []string{}
		for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
			if value := os.Getenv(key); value != "" {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			return nil, fmt.Errorf("TEMPORARY_FAILURE: scraper control channel unavailable")
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			stdin.Close()
			return nil, fmt.Errorf("TEMPORARY_FAILURE: scraper control channel unavailable")
		}
		if err = cmd.Start(); err != nil {
			stdin.Close()
			stdout.Close()
			return nil, fmt.Errorf("UNSUPPORTED: scraper worker failed to start")
		}
		contain, err = containProcess(cmd.Process.Pid)
		if err != nil {
			cmd.Process.Kill()
			cmd.Wait()
			stderr.Close()
			return nil, fmt.Errorf("UNSUPPORTED: scraper process containment unavailable")
		}
		peer = pluginproto.NewPeer(ctx, stdout, stdin, "h-", nil)
		go func() { _ = cmd.Wait(); _ = stderr.Close(); cancel(); _ = peer.Close(); close(processDone) }()
	}
	cleaned := make(chan struct{})
	go func() {
		<-ctx.Done()
		_ = peer.Close()
		channel.Close()
		contain()
		<-processDone
		<-h.slots
		close(cleaned)
	}()
	release = false
	session := &Session{ctx: ctx, cancel: cancel, closed: cleaned, peer: peer, channel: channel, scope: scope, target: target, resources: resources}
	defer func() {
		if failed {
			session.Close()
		}
	}()
	startup, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	var ready pluginproto.Ready
	hello := helloRequest{Hello: pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: scope.InstanceID, Nonce: nonce, ConfigRevision: "1"}, Context: scope, Target: target, BaseURL: channel.base}
	if err = peer.Call(startup, "host.hello", hello, &ready); err != nil {
		return nil, err
	}
	if pluginconfig.ValidateManifest(ready.Manifest, "scraper") != nil || ready.Manifest.PluginID != PluginID || !slices.Contains(ready.Manifest.Capabilities, "scrape.search") || !slices.Contains(ready.Manifest.Capabilities, "scrape.fetch") || ready.Nonce != nonce || ready.InstanceID != scope.InstanceID {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: scraper worker identity mismatch")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	failed = false
	session.version = ready.Manifest.PluginVersion
	version := session.version
	h.version.Store(&version)
	return session, nil
}

// PluginVersion returns only a release verified by a completed worker handshake.
func (h *Host) PluginVersion() string {
	if version := h.version.Load(); version != nil {
		return *version
	}
	return ""
}

func (s *Session) PluginVersion() string { return s.version }

func (s *Session) Close() error { s.cancel(); <-s.closed; return nil }

func verifyWorker(config Config) error {
	if config.Builtin {
		return nil
	}
	if !filepath.IsAbs(config.WorkerPath) || len(config.WorkerSHA256) != 64 {
		return fmt.Errorf("ACCESS_DENIED: invalid scraper executable identity")
	}
	f, err := os.Open(config.WorkerPath)
	if err != nil {
		return fmt.Errorf("UNSUPPORTED: scraper executable unavailable")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("ACCESS_DENIED: invalid scraper executable")
	}
	hash := sha256.New()
	if _, err = io.Copy(hash, f); err != nil || hex.EncodeToString(hash.Sum(nil)) != config.WorkerSHA256 {
		return fmt.Errorf("ACCESS_DENIED: scraper executable checksum mismatch")
	}
	return nil
}
