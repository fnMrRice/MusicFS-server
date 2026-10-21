package pluginrpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"reflect"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const Protocol = "musicfs-persistence/1"
const MaxMessageBytes = 64 << 20

type ProcessConfig struct {
	Executable, SHA256, WorkDir string
	Args                        []string
}

type hello struct {
	Protocol, Contract, Nonce, Mode string
	Options                         json.RawMessage
}

type ready struct {
	Protocol, Contract, Nonce string
	PluginVersion             string `json:"pluginVersion"`
}

type Process struct {
	*Endpoint
	cancel      context.CancelFunc
	done        chan struct{}
	once        sync.Once
	initialized bool
	version     string
}

func Start(ctx context.Context, cfg ProcessConfig, contract reflect.Type, mode string, options any) (*Process, error) {
	if contract == nil || contract.Kind() != reflect.Interface {
		return nil, domain.Fail("INVALID_ARGUMENT", "Invalid persistence contract")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.Open(cfg.Executable)
	if err != nil {
		return nil, domain.Fail("ACCESS_DENIED", "Persistence plugin executable is unavailable")
	}
	hash := sha256.New()
	_, err = io.Copy(hash, file)
	file.Close()
	if err != nil || len(cfg.SHA256) != 64 || hex.EncodeToString(hash.Sum(nil)) != cfg.SHA256 {
		return nil, domain.Fail("ACCESS_DENIED", "Persistence plugin checksum mismatch")
	}
	body, err := json.Marshal(options)
	if err != nil {
		return nil, err
	}
	// The backend must remain available while server shutdown drains workers and
	// persists their final state. Only Close owns the process lifetime; the
	// caller's context still bounds initialization and every individual operation.
	runCtx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(runCtx, cfg.Executable, cfg.Args...)
	cmd.Dir, cmd.Stderr, cmd.WaitDelay = cfg.WorkDir, io.Discard, 2*time.Second
	configureProcess(cmd)
	cmd.Env = []string{}
	for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
		if v, exists := os.LookupEnv(key); exists {
			cmd.Env = append(cmd.Env, key+"="+v)
		}
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		cancel()
		return nil, err
	}
	if err = cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		cancel()
		return nil, domain.Fail("TEMPORARY_FAILURE", "Persistence plugin failed to start")
	}
	cleanup, err := containProcess(cmd.Process.Pid)
	if err != nil {
		cancel()
		cmd.Process.Kill()
		cmd.Wait()
		return nil, domain.Fail("UNSUPPORTED", "Persistence process containment is unavailable")
	}
	p := &Process{Endpoint: NewEndpoint(), cancel: cancel, done: make(chan struct{})}
	_ = p.SetContract(contract)
	p.Peer = pluginproto.NewPeerWithLimit(runCtx, stdout, stdin, "h-", p.Handle, MaxMessageBytes)
	go func() {
		cmd.Wait()
		p.Peer.Close()
		cleanup()
		close(p.done)
	}()
	startup, end := context.WithTimeout(ctx, 15*time.Second)
	defer end()
	h := hello{Protocol: Protocol, Contract: Fingerprint(contract), Nonce: domain.NewID(), Mode: mode, Options: body}
	var r ready
	if err = p.Peer.Call(startup, "persistence.hello", h, &r); err != nil ||
		r.Protocol != h.Protocol || r.Contract != h.Contract || r.Nonce != h.Nonce || !pluginproto.ValidPluginVersion(r.PluginVersion) {
		p.Close()
		if startup.Err() != nil {
			return nil, startup.Err()
		}
		return nil, domain.Fail("PROTOCOL_MISMATCH", "Persistence plugin handshake failed")
	}
	p.initialized = true
	p.version = r.PluginVersion
	return p, nil
}

func (p *Process) PluginVersion() string { return p.version }

// Available reports established connection health without issuing an RPC.
func (p *Process) Available() bool {
	if p == nil || !p.initialized || p.Endpoint == nil || p.Peer == nil {
		return false
	}
	select {
	case <-p.Peer.Done():
		return false
	default:
		return true
	}
}

func (p *Process) Close() error {
	p.once.Do(func() {
		p.Peer.Close()
		p.cancel()
		<-p.done
	})
	return nil
}

// Factory must finish initialization before returning. It may only select modes
// defined by its SDK. Options come from the trusted host's deployment settings.
type Factory func(context.Context, string, json.RawMessage) (any, reflect.Type, io.Closer, error)

func Serve(ctx context.Context, input io.ReadCloser, output io.WriteCloser, version string, contracts map[string]reflect.Type, factory Factory) error {
	if !pluginproto.ValidPluginVersion(version) {
		return fmt.Errorf("invalid persistence plugin release version")
	}
	e := NewEndpoint()
	var mu sync.Mutex
	var resource io.Closer
	started := false
	initialized := false
	handler := func(callCtx context.Context, method string, raw json.RawMessage) (any, error) {
		if method != "persistence.hello" {
			mu.Lock()
			ok := initialized
			mu.Unlock()
			if !ok {
				return nil, fmt.Errorf("persistence handshake required")
			}
			return e.Handle(callCtx, method, raw)
		}
		mu.Lock()
		defer mu.Unlock()
		if started {
			return nil, fmt.Errorf("duplicate persistence handshake")
		}
		started = true
		var h hello
		if json.Unmarshal(raw, &h) != nil || h.Protocol != Protocol || len(h.Nonce) != 48 {
			return nil, fmt.Errorf("unsupported persistence handshake")
		}
		expected := contracts[h.Mode]
		if expected == nil || expected.Kind() != reflect.Interface || h.Contract != Fingerprint(expected) {
			return nil, fmt.Errorf("persistence contract differs")
		}
		service, contract, closer, err := factory(callCtx, h.Mode, h.Options)
		resource = closer
		if err != nil {
			return nil, fmt.Errorf("persistence initialization failed")
		}
		if contract == nil || contract.Kind() != reflect.Interface {
			return nil, fmt.Errorf("invalid persistence contract")
		}
		if contract != expected {
			return nil, fmt.Errorf("persistence contract differs")
		}
		if err = e.Bind(service, contract); err != nil {
			return nil, err
		}
		initialized = true
		return ready{Protocol: Protocol, Contract: h.Contract, Nonce: h.Nonce, PluginVersion: version}, nil
	}
	e.Peer = pluginproto.NewPeerWithLimit(ctx, input, output, "w-", handler, MaxMessageBytes)
	<-e.Peer.Done()
	mu.Lock()
	defer mu.Unlock()
	if resource != nil {
		resource.Close()
	}
	return nil
}
