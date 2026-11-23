package pluginhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online"
	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
	"github.com/fnMrRice/MusicFS-server/src/plugins/accountstore"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginconfig"
)

type OnlineConfig struct {
	Builtin                                                    bool
	Args                                                       []string
	Executable, SHA256, StateDir, Platform, UserID, InstanceID string
	Disabled                                                   bool
	Timeout                                                    time.Duration
}

type OnlineWorker struct {
	accountMu      sync.Mutex
	restorePending atomic.Bool
	accountDisplay accountDisplaySnapshot
	accountStore   *accountstore.Store
	config         OnlineConfig
	peer           *pluginproto.Peer
	cancel         context.CancelFunc
	done           chan struct{}
	manifest       pluginproto.Manifest
	descriptor     pluginproto.OnlineDescriptor
	version        string
}

func StartOnline(parent context.Context, config OnlineConfig) (worker *OnlineWorker, startErr error) {
	parent = pluginlog.Context(parent, pluginproto.OnlinePluginID(config.Platform), config.InstanceID)
	finish := pluginlog.Operation(parent, "worker.start")
	defer func() { finish(startErr) }()
	config.Args = slices.Clone(config.Args)
	if err := parent.Err(); err != nil {
		return nil, err
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if pluginproto.OnlinePluginID(config.Platform) == "" || !pluginproto.OnlineIdentity(config.UserID) || !pluginproto.OnlineIdentity(config.InstanceID) || config.Timeout < time.Second || config.Timeout > 30*time.Second {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid online worker configuration")
	}
	base := Config{Builtin: config.Builtin, Executable: config.Executable, SHA256: config.SHA256, StateDir: config.StateDir, UserID: config.UserID, InstanceID: config.InstanceID, Disabled: config.Disabled}
	if err := base.Validate(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Online worker directory is unavailable")
	}
	privateDir, err := os.MkdirTemp(config.StateDir, "online-")
	if err != nil {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Cannot create online worker directory")
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(privateDir)
		}
	}()
	ctx, cancel := context.WithCancel(parent)
	w := &OnlineWorker{config: config, cancel: cancel, done: make(chan struct{})}
	// 插件进程退出时，其账号代次授权的缓存读取也必须结束。
	context.AfterFunc(ctx, w.accountDisplay.invalidate)
	if config.Builtin {
		local := inprocess.Start(ctx, func(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
			return onlineplugins.Run(ctx, config.Platform, input, output)
		})
		w.peer = pluginproto.NewPeer(ctx, local.Output, local.Input, "h-", nil)
		go func() { <-w.peer.Done(); cancel() }()
		go func() {
			<-local.Done
			cancel()
			_ = w.peer.Close()
			_ = os.Remove(privateDir)
			close(w.done)
		}()
	} else {
		cmd := exec.CommandContext(ctx, config.Executable, config.Args...)
		onlineConfigureProcess(cmd)
		stderr := logging.PluginStderr(pluginproto.OnlinePluginID(config.Platform), config.InstanceID)
		cmd.Dir, cmd.Stderr, cmd.WaitDelay = privateDir, stderr, time.Second
		defer func() {
			if cmd.Process == nil {
				_ = stderr.Close()
			}
		}()
		cmd.Env = []string{}
		for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
			if v := os.Getenv(key); v != "" {
				cmd.Env = append(cmd.Env, key+"="+v)
			}
		}
		// 环境授权来自受信任插件装配；Worker 描述符不能申请其它宿主环境变量。
		for _, key := range onlineplugins.Plugin(config.Platform, onlineplugins.Options{}).Environment {
			if value := os.Getenv(key); value != "" {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		stdin, err := cmd.StdinPipe()
		if err != nil {
			cancel()
			return nil, domain.Fail("TEMPORARY_FAILURE", "Online control channel is unavailable")
		}
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			cancel()
			stdin.Close()
			return nil, domain.Fail("TEMPORARY_FAILURE", "Online control channel is unavailable")
		}
		if err = cmd.Start(); err != nil {
			cancel()
			stdin.Close()
			stdout.Close()
			return nil, domain.Fail("SOURCE_OFFLINE", "Online worker failed to start")
		}
		contain, err := onlineContainProcess(cmd.Process.Pid)
		if err != nil {
			cancel()
			stdin.Close()
			stdout.Close()
			cmd.Process.Kill()
			cmd.Wait()
			stderr.Close()
			return nil, domain.Fail("UNSUPPORTED", "Online process containment is unavailable")
		}
		w.peer = pluginproto.NewPeer(ctx, stdout, stdin, "h-", nil)
		go func() { <-w.peer.Done(); cancel() }()
		go func() {
			_ = cmd.Wait()
			_ = stderr.Close()
			cancel()
			_ = w.peer.Close()
			contain()
			_ = os.Remove(privateDir)
			close(w.done)
		}()
	}
	cleanup = false
	failed := true
	defer func() {
		if failed {
			w.Close()
		}
	}()
	nonce, err := onlineRandom()
	if err != nil {
		return nil, err
	}
	hello := pluginproto.OnlineHello{Hello: pluginproto.Hello{ProtocolVersion: pluginproto.ProtocolVersion, InstanceID: config.InstanceID, Nonce: nonce, ConfigRevision: pluginproto.OnlineRevision}, UserID: config.UserID, TimeoutMs: config.Timeout.Milliseconds()}
	startup, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	var raw json.RawMessage
	if err = w.peer.Call(startup, "host.hello", hello, &raw); err != nil {
		return nil, onlineError(startup, err)
	}
	var ready pluginproto.Ready
	if pluginproto.DecodeOnline(raw, &ready) != nil || ready.Nonce != nonce || ready.InstanceID != config.InstanceID || pluginconfig.ValidateManifest(ready.Manifest, "provider") != nil || pluginproto.ValidateOnlineManifest(ready.Manifest, config.Platform) != nil || !slices.Contains(ready.Manifest.Systems, runtime.GOOS) || !slices.Contains(ready.Manifest.Architectures, runtime.GOARCH) {
		return nil, domain.Fail("PROTOCOL_MISMATCH", "Online worker handshake mismatch")
	}
	w.manifest = ready.Manifest
	w.descriptor, err = w.Describe(startup)
	if err != nil {
		return nil, err
	}
	if err = pluginproto.ValidateOnlineDescriptor(ready.Manifest, w.descriptor, config.Platform); err != nil {
		return nil, domain.Fail("PROTOCOL_MISMATCH", "Online plugin descriptor does not match the handshake")
	}
	w.version = ready.Manifest.PluginVersion
	if err = w.initializeAccount(startup); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	failed = false
	pluginlog.Logger(parent).InfoContext(parent, "Plugin worker ready", "builtin", config.Builtin, "event", "plugin.lifecycle.started")
	return w, nil
}

func onlineRandom() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", domain.Fail("TEMPORARY_FAILURE", "Cannot initialize online request identity")
	}
	return hex.EncodeToString(b[:]), nil
}

func (w *OnlineWorker) Close() error {
	pluginlog.Logger(pluginlog.Context(context.Background(), pluginproto.OnlinePluginID(w.config.Platform), w.config.InstanceID)).Debug("Plugin worker stopping", "event", "plugin.lifecycle.stopping")
	w.cancel()
	_ = w.peer.Close()
	select {
	case <-w.done:
		return nil
	case <-time.After(3 * time.Second):
		return domain.Fail("TEMPORARY_FAILURE", "Online worker did not stop within deadline")
	}
}
func (w *OnlineWorker) Done() <-chan struct{} { return w.done }
func (w *OnlineWorker) Descriptor() pluginproto.OnlineDescriptor {
	return w.descriptor.Clone()
}

func (w *OnlineWorker) PluginIdentity() *pluginproto.PluginIdentity {
	return w.descriptor.Plugin.Clone()
}

func (w *OnlineWorker) PluginVersion() string { return w.version }

// Keep paths private; only identity and verified capabilities are public.
func (w *OnlineWorker) Platform() string { return w.config.Platform }
