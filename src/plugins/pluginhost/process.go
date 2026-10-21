package pluginhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
)

type Worker struct {
	config       Config
	Access       *sourceaccess.Access
	Manifest     pluginproto.Manifest
	identity     *pluginproto.PluginIdentity
	displayCount *plugindisplay.Count
	peer         *pluginproto.Peer
	cmd          *exec.Cmd
	cancel       context.CancelFunc
	done         chan struct{}
	privateDir   string
	state        *stateStore
	mu           sync.Mutex
	snapshots    map[string]entrySnapshot
	scanGrants   map[string]scanGrant
}

func Start(ctx context.Context, cfg Config) (worker *Worker, startErr error) {
	ctx = pluginlog.Context(ctx, "musicfs.plugin.source.localfiles", cfg.InstanceID)
	finish := pluginlog.Operation(ctx, "worker.start")
	defer func() { finish(startErr) }()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.StateDir, 0700); err != nil {
		return nil, err
	}
	state := &stateStore{dir: filepath.Join(cfg.StateDir, "plugin-state")}
	previous, err := state.load("root-identities")
	if err != nil {
		return nil, err
	}
	var known map[string]string
	if len(previous.Value) > 0 {
		if err = json.Unmarshal(previous.Value, &known); err != nil {
			return nil, err
		}
	}
	for i := range cfg.Roots {
		if expected := known[cfg.Roots[i].ID]; expected != "" {
			cfg.Roots[i].ExpectedIdentity = expected
		}
	}
	access, err := sourceaccess.New(cfg.Roots)
	if err != nil {
		return nil, err
	}
	identities := map[string]string{}
	for _, root := range access.Roots() {
		identities[root.ID] = root.ExpectedIdentity
	}
	b, _ := json.Marshal(identities)
	if _, err = state.save(pluginproto.StateRequest{Key: "root-identities", Revision: previous.Revision, Value: b}); err != nil {
		return nil, err
	}
	privateDir, err := os.MkdirTemp(cfg.StateDir, "worker-")
	if err != nil {
		return nil, err
	}
	workerCtx, cancel := context.WithCancel(ctx)
	w := &Worker{config: cfg, Access: access, cancel: cancel, done: make(chan struct{}), privateDir: privateDir, state: state, snapshots: map[string]entrySnapshot{}, scanGrants: map[string]scanGrant{}}
	if cfg.Builtin {
		local := inprocess.Start(workerCtx, localfiles.Run)
		w.peer = pluginproto.NewPeer(workerCtx, local.Output, local.Input, "h-", w.handle)
		go func() {
			<-local.Done
			cancel()
			w.peer.Close()
			w.peer.Wait()
			close(w.done)
			os.Remove(privateDir)
		}()
	} else {
		cmd := exec.CommandContext(workerCtx, cfg.Executable, cfg.Args...)
		cmd.Dir = privateDir
		cmd.Env = []string{}
		for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
			if v := os.Getenv(key); v != "" {
				cmd.Env = append(cmd.Env, key+"="+v)
			}
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			os.Remove(privateDir)
			return nil, err
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			os.Remove(privateDir)
			return nil, err
		}
		stderr := logging.PluginStderr("musicfs.plugin.source.localfiles", cfg.InstanceID)
		cmd.Stderr = stderr
		defer func() {
			if cmd.Process == nil {
				_ = stderr.Close()
			}
		}()
		cmd.WaitDelay = 2 * time.Second
		if err = cmd.Start(); err != nil {
			cancel()
			os.Remove(privateDir)
			return nil, fmt.Errorf("UNSUPPORTED: worker failed to start")
		}
		w.cmd = cmd
		w.peer = pluginproto.NewPeer(workerCtx, stdout, stdin, "h-", w.handle)
		go func() {
			cmd.Wait()
			stderr.Close()
			w.peer.Close()
			w.peer.Wait()
			close(w.done)
			os.Remove(privateDir)
		}()
	}
	var random [24]byte
	rand.Read(random[:])
	nonce := hex.EncodeToString(random[:])
	handshake, cancelHandshake := context.WithTimeout(ctx, 5*time.Second)
	defer cancelHandshake()
	var ready pluginproto.Ready
	err = w.peer.Call(handshake, "host.hello", pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: cfg.InstanceID, Nonce: nonce, ConfigRevision: "1"}, &ready)
	if err == nil {
		err = pluginconfig.ValidateManifest(ready.Manifest, "provider")
	}
	if err == nil && (ready.InstanceID != cfg.InstanceID || ready.Nonce != nonce || ready.Manifest.PluginID != "musicfs.plugin.source.localfiles") {
		err = fmt.Errorf("PROTOCOL_MISMATCH: invalid handshake")
	}
	if err == nil && (ready.Manifest.Plugin == nil || !slices.Contains(ready.Manifest.Plugin.Features, "music-source")) {
		err = fmt.Errorf("PROTOCOL_MISMATCH: music source declaration is required")
	}
	if err != nil {
		w.Close()
		return nil, err
	}
	w.Manifest = ready.Manifest
	pluginlog.Logger(ctx).InfoContext(ctx, "Plugin worker ready", "builtin", cfg.Builtin, "event", "plugin.lifecycle.started")
	w.identity = ready.Manifest.Plugin.Clone()
	displayContext, cancelDisplay := context.WithTimeout(ctx, time.Second)
	_ = w.loadDisplayCount(displayContext)
	cancelDisplay()
	return w, nil
}

// PluginIdentity returns only the verified declaration cached at startup.
func (w *Worker) PluginIdentity() *pluginproto.PluginIdentity { return w.identity.Clone() }

func (w *Worker) Scan(ctx context.Context, r pluginproto.ScanRequest) (pluginproto.ScanPage, error) {
	if err := w.state.ready(); err != nil {
		return pluginproto.ScanPage{}, err
	}
	grant, err := w.authorizeScan(ctx, &r)
	if err != nil {
		return pluginproto.ScanPage{}, err
	}
	defer func() { w.mu.Lock(); delete(w.scanGrants, grant); w.mu.Unlock() }()
	var page pluginproto.ScanPage
	err = w.peer.Call(ctx, "provider.scan", r, &page)
	if err == nil && page.PlanDigest != r.PlanDigest {
		err = fmt.Errorf("PROTOCOL_MISMATCH: scan result belongs to another plan")
	}
	if err == nil {
		err = w.validateScanReceipt(grant, &page)
	}
	return page, err
}
func (w *Worker) Resolve(ctx context.Context, r pluginproto.ResolveRequest) (pluginproto.Resource, error) {
	if err := w.state.ready(); err != nil {
		return pluginproto.Resource{}, err
	}
	var result pluginproto.Resource
	err := w.peer.Call(ctx, "provider.resolve", r, &result)
	return result, err
}
func (w *Worker) Close() error {
	pluginlog.Logger(w.peerContext()).Debug("Plugin worker stopping", "event", "plugin.lifecycle.stopping")
	w.cancel()
	w.peer.Close()
	select {
	case <-w.done:
		return nil
	case <-time.After(3 * time.Second):
		return fmt.Errorf("worker did not stop within deadline")
	}
}

func (w *Worker) peerContext() context.Context {
	return pluginlog.Context(context.Background(), "musicfs.plugin.source.localfiles", w.config.InstanceID)
}
func (w *Worker) Done() <-chan struct{} { return w.done }
