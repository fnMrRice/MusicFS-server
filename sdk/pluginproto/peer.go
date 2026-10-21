package pluginproto

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
)

type Handler func(context.Context, string, json.RawMessage) (any, error)
type Peer struct {
	framer   *Framer
	reader   io.ReadCloser
	writer   io.WriteCloser
	prefix   string
	handler  Handler
	mu       sync.Mutex
	pending  map[string]chan Envelope
	active   map[string]context.CancelFunc
	terminal map[string]bool
	count    atomic.Uint64
	done     chan struct{}
	readDone chan struct{}
	requests sync.WaitGroup
	once     sync.Once
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
}

func NewPeer(ctx context.Context, r io.ReadCloser, w io.WriteCloser, prefix string, handler Handler) *Peer {
	return NewPeerWithLimit(ctx, r, w, prefix, handler, MaxMessageBytes)
}

func NewPeerWithLimit(ctx context.Context, r io.ReadCloser, w io.WriteCloser, prefix string, handler Handler, limit int) *Peer {
	ctx, cancel := context.WithCancel(ctx)
	p := &Peer{framer: NewFramerWithLimit(r, w, limit), reader: r, writer: w, prefix: prefix, handler: handler, pending: map[string]chan Envelope{}, active: map[string]context.CancelFunc{}, terminal: map[string]bool{}, done: make(chan struct{}), readDone: make(chan struct{}), ctx: ctx, cancel: cancel}
	go p.readLoop()
	go func() {
		select {
		case <-ctx.Done():
			p.fail(ctx.Err())
		case <-p.done:
		}
	}()
	return p
}

func (p *Peer) Done() <-chan struct{} { return p.done }
func (p *Peer) Err() error            { p.mu.Lock(); defer p.mu.Unlock(); return p.err }
func (p *Peer) Close() error          { p.fail(io.EOF); return nil }

// Wait 等待协议关闭和已取消的请求退出，确保进程内实例不遗留任务。
func (p *Peer) Wait() {
	<-p.done
	<-p.readDone
	p.requests.Wait()
}

func (p *Peer) fail(err error) {
	p.once.Do(func() {
		p.mu.Lock()
		p.err = err
		for _, cancel := range p.active {
			cancel()
		}
		p.mu.Unlock()
		p.cancel()
		p.reader.Close()
		p.writer.Close()
		close(p.done)
	})
}

func (p *Peer) Call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	id := p.prefix + strconv.FormatUint(p.count.Add(1), 10)
	ch := make(chan Envelope, 1)
	p.mu.Lock()
	if len(p.pending) >= 16 {
		p.mu.Unlock()
		return Failure("RATE_LIMITED", "too many in-flight requests")
	}
	p.pending[id] = ch
	p.mu.Unlock()
	if err = p.framer.Write(Envelope{ID: id, Method: method, Params: body}); err != nil {
		p.fail(err)
	}
	select {
	case e := <-ch:
		if e.Error != nil {
			return e.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(e.Result, result)
	case <-ctx.Done():
		p.mu.Lock()
		delete(p.pending, id)
		p.terminal[id] = false
		p.mu.Unlock()
		b, _ := json.Marshal(map[string]string{"id": id})
		if err := p.framer.Write(Envelope{Method: "$/cancelRequest", Params: b}); err != nil {
			p.fail(err)
		}
		return ctx.Err()
	case <-p.done:
		return p.Err()
	}
}

func (p *Peer) readLoop() {
	defer close(p.readDone)
	for {
		e, err := p.framer.Read()
		if err != nil {
			p.fail(err)
			return
		}
		if e.Method == "" {
			p.mu.Lock()
			ch, ok := p.pending[e.ID]
			if ok {
				delete(p.pending, e.ID)
				p.terminal[e.ID] = true
			}
			late, known := p.terminal[e.ID]
			if !ok && known && !late {
				p.terminal[e.ID] = true
			}
			if len(p.terminal) > 4096 {
				for id := range p.terminal {
					if id != e.ID {
						delete(p.terminal, id)
					}
					if len(p.terminal) <= 2048 {
						break
					}
				}
			}
			p.mu.Unlock()
			if ok {
				ch <- e
			} else if !known || late {
				p.fail(fmt.Errorf("PROTOCOL_MISMATCH: unknown or duplicate terminal response"))
				return
			}
			continue
		}
		if e.Method == "$/cancelRequest" {
			var request struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(e.Params, &request) != nil {
				p.fail(fmt.Errorf("PROTOCOL_MISMATCH: invalid cancellation"))
				return
			}
			p.mu.Lock()
			cancel := p.active[request.ID]
			p.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			continue
		}
		if e.ID == "" {
			p.fail(fmt.Errorf("PROTOCOL_MISMATCH: unsupported notification"))
			return
		}
		p.mu.Lock()
		_, duplicate := p.active[e.ID]
		if duplicate || len(p.active) >= 16 {
			p.mu.Unlock()
			p.fail(fmt.Errorf("PROTOCOL_MISMATCH: duplicate or excessive request"))
			return
		}
		ctx, cancel := context.WithCancel(p.ctx)
		p.active[e.ID] = cancel
		p.mu.Unlock()
		p.requests.Add(1)
		go p.respond(ctx, cancel, e)
	}
}

func (p *Peer) respond(ctx context.Context, cancel context.CancelFunc, e Envelope) {
	defer p.requests.Done()
	defer cancel()
	var result any
	var err error
	if p.handler == nil {
		err = Failure("UNSUPPORTED", "unknown method")
	} else {
		result, err = p.handler(ctx, e.Method, e.Params)
	}
	response := Envelope{ID: e.ID}
	if err != nil {
		if typed, ok := err.(*RPCError); ok {
			response.Error = typed
		} else {
			response.Error = Failure(ErrorCode(err), "worker request failed")
		}
	} else {
		response.Result, err = json.Marshal(result)
		if err != nil {
			response.Error = Failure("TEMPORARY_FAILURE", "invalid result")
		}
	}
	p.mu.Lock()
	delete(p.active, e.ID)
	p.mu.Unlock()
	if err := p.framer.Write(response); err != nil {
		p.fail(err)
	}
}
