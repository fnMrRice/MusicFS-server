package dspworker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/dsp/gain"
	"github.com/fnMrRice/MusicFS-server/sdk/bundledplugin"
	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/audio"
)

// Process 为一次播放中的一个节点维护独立的协议会话。
type Process struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	cmd     *exec.Cmd
	input   io.WriteCloser
	output  io.ReadCloser
	done    chan struct{}
	cleanup func()
	stop    sync.Once
	timeout time.Duration
	version string
}

func New(ctx context.Context, cfg Config, params map[string]float64) (*Process, error) {
	if err := cfg.VerifyExecutable(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	p := &Process{ctx: ctx, cancel: cancel, done: make(chan struct{}), cleanup: func() {}, timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond}
	if p.timeout == 0 {
		p.timeout = 5 * time.Second
	}
	if cfg.Builtin {
		local := inprocess.Start(ctx, func(ctx context.Context, input io.ReadCloser, output io.WriteCloser) error {
			factory := func(params map[string]float64) (audio.Node, error) {
				registry := audio.NewRegistry()
				if err := dsp.Register(registry); err != nil {
					return nil, err
				}
				return registry.Create("gain", params)
			}
			return Serve(ctx, input, output, cfg.Name, bundledplugin.GainVersion, factory)
		})
		p.input, p.output = local.Input, local.Output
		p.cleanup = func() { _ = local.Close() }
		go func() { <-local.Done; close(p.done) }()
	} else {
		cmd := exec.CommandContext(ctx, cfg.Executable, cfg.Args...)
		p.cmd = cmd
		configureProcess(cmd)
		cmd.Stderr = io.Discard
		cmd.WaitDelay = time.Second
		cmd.Env = []string{}
		for _, key := range []string{"SystemRoot", "WINDIR", "TEMP", "TMP"} {
			if value := os.Getenv(key); value != "" {
				cmd.Env = append(cmd.Env, key+"="+value)
			}
		}
		var err error
		p.input, err = cmd.StdinPipe()
		if err == nil {
			p.output, err = cmd.StdoutPipe()
		}
		if err == nil {
			err = cmd.Start()
		}
		if err != nil {
			cancel()
			if p.input != nil {
				p.input.Close()
			}
			if p.output != nil {
				p.output.Close()
			}
			return nil, fmt.Errorf("UNSUPPORTED: DSP worker failed to start")
		}
		p.cleanup, err = containProcess(cmd.Process.Pid)
		if err != nil {
			cancel()
			cmd.Process.Kill()
			cmd.Wait()
			return nil, fmt.Errorf("UNSUPPORTED: DSP worker containment unavailable")
		}
		go func() { cmd.Wait(); close(p.done) }()
	}
	go func() {
		select {
		case <-ctx.Done():
			p.shutdown()
		case <-p.done:
			p.shutdown()
		}
	}()
	data, err := p.exchange(frameHello, marshal(hello{ProtocolVersion: ProtocolVersion, Name: cfg.Name, NodeVersion: audio.NodeVersion, Format: audio.Canonical, Params: params}))
	var response ready
	if err == nil && (json.Unmarshal(data, &response) != nil || !pluginproto.ValidPluginVersion(response.PluginVersion) || response.ProtocolVersion != ProtocolVersion || response.NodeVersion != audio.NodeVersion || response.Name != cfg.Name || response.Format != audio.Canonical || response.DelayFrames != 0 || response.TailFrames != 0 || !response.PreservesTiming || !response.Stateless) {
		err = fmt.Errorf("PROTOCOL_MISMATCH: unsupported DSP worker contract")
	}
	if err != nil {
		p.Close()
		return nil, err
	}
	p.version = response.PluginVersion
	return p, nil
}

func (p *Process) PluginVersion() string { return p.version }

func (p *Process) Configure(format audio.Format) error {
	if format != audio.Canonical {
		return fmt.Errorf("UNSUPPORTED: unsupported DSP format")
	}
	return p.ctx.Err()
}

func (p *Process) Process(block *audio.Block) error {
	data, err := encodeBlock(block)
	if err != nil {
		return err
	}
	response, err := p.exchange(frameProcess, data)
	if err != nil {
		return err
	}
	next, err := decodeBlock(response)
	if err != nil {
		p.Close()
		return err
	}
	if next.Frames != block.Frames || next.Sequence != block.Sequence || next.PositionFrames != block.PositionFrames || next.End != block.End || next.Discontinuity != block.Discontinuity {
		p.Close()
		return fmt.Errorf("PROTOCOL_MISMATCH: DSP changed block timing")
	}
	copy(block.Samples, next.Samples)
	return nil
}

func (p *Process) Reset() error     { _, err := p.exchange(frameReset, nil); return err }
func (p *Process) Drain() error     { _, err := p.exchange(frameDrain, nil); return err }
func (*Process) DelayFrames() int64 { return 0 }
func (*Process) Stateless() bool    { return true }

func (p *Process) Close() error {
	p.shutdown()
	<-p.done
	return nil
}

func (p *Process) shutdown() {
	p.stop.Do(func() {
		p.cancel()
		p.cleanup()
		if p.cmd != nil {
			p.cmd.Process.Kill()
		}
		p.input.Close()
		p.output.Close()
	})
}

func (p *Process) exchange(kind uint32, payload []byte) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.ctx.Err(); err != nil {
		return nil, err
	}
	type result struct {
		kind uint32
		data []byte
		err  error
	}
	results := make(chan result, 1)
	go func() {
		if err := writeFrame(p.input, kind, payload); err != nil {
			results <- result{err: err}
			return
		}
		k, data, err := readFrame(p.output)
		results <- result{kind: k, data: data, err: err}
	}()
	timer := time.NewTimer(p.timeout)
	defer timer.Stop()
	select {
	case value := <-results:
		if value.err != nil || value.kind != kind || (kind == frameReset || kind == frameDrain) && len(value.data) != 0 {
			p.shutdown()
			return nil, fmt.Errorf("PROTOCOL_MISMATCH: DSP worker response failed")
		}
		return value.data, nil
	case <-p.ctx.Done():
		p.shutdown()
		return nil, p.ctx.Err()
	case <-timer.C:
		p.shutdown()
		return nil, fmt.Errorf("TEMPORARY_FAILURE: DSP worker timed out")
	}
}
