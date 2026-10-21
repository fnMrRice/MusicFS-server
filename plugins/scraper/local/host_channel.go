package scraperlocal

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/inprocess"
)

type outputGrant struct {
	writer  io.Writer
	size    int64
	done    chan error
	started bool
}
type hostChannel struct {
	base     string
	ctx      context.Context
	server   *http.Server
	inputs   map[string]Input
	mu       sync.Mutex
	outputs  map[string]*outputGrant
	handlers inprocess.Handlers
}

func newHostChannel(ctx context.Context, nonce string, inputs []Input) (*hostChannel, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("TEMPORARY_FAILURE: scraper channel unavailable")
	}
	host := listener.Addr().String()
	prefix := "/" + nonce
	channel := &hostChannel{base: "http://" + host + prefix, ctx: ctx, inputs: map[string]Input{}, outputs: map[string]*outputGrant{}}
	for _, input := range inputs {
		channel.inputs[input.Resource.ResourceRef] = input
	}
	slots := make(chan struct{}, 4)
	server := &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ctx.Err() != nil {
			w.WriteHeader(http.StatusGone)
			return
		}
		if r.Host != host || r.URL.RawQuery != "" || r.URL.ForceQuery || r.URL.RawPath != "" || len(r.TransferEncoding) != 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if strings.HasPrefix(r.URL.Path, prefix+"/input/") && r.Method == http.MethodGet && r.ContentLength == 0 {
			ref := strings.TrimPrefix(r.URL.Path, prefix+"/input/")
			channel.serveInput(w, r, ref)
			return
		}
		if strings.HasPrefix(r.URL.Path, prefix+"/output/") && r.Method == http.MethodPost {
			ref := strings.TrimPrefix(r.URL.Path, prefix+"/output/")
			channel.serveOutput(w, r, ref)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	})
	channel.server = server
	server.Handler = channel.handlers.Wrap(server.Handler)
	limited := &channelListener{Listener: listener, slots: make(chan struct{}, 8), done: make(chan struct{})}
	go func() { _ = server.Serve(limited) }()
	return channel, nil
}

func (c *hostChannel) Close() { _ = c.server.Close(); c.handlers.Wait() }
func (c *hostChannel) Validate() error {
	if err := c.ctx.Err(); err != nil {
		return err
	}
	for _, input := range c.inputs {
		if err := input.Validate(); err != nil {
			return fmt.Errorf("RESOURCE_CHANGED: scraper source no longer current")
		}
	}
	return nil
}

func (c *hostChannel) serveInput(w http.ResponseWriter, r *http.Request, ref string) {
	input, ok := c.inputs[ref]
	if !ok {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	start, end, ok := exactRange(r.Header.Values("Range"), input.Resource.SizeBytes)
	if !ok {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	if err := input.Validate(); err != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	body := make([]byte, int(end-start+1))
	n, err := input.Reader.ReadAt(body, start)
	if n != len(body) || (err != nil && err != io.EOF) || input.Validate() != nil {
		w.WriteHeader(http.StatusConflict)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, input.Resource.SizeBytes))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(body)
}

func (c *hostChannel) serveOutput(w http.ResponseWriter, r *http.Request, ref string) {
	c.mu.Lock()
	grant, ok := c.outputs[ref]
	if !ok || grant.started || r.ContentLength != grant.size || len(r.Header.Values("Content-Encoding")) != 0 {
		c.mu.Unlock()
		w.WriteHeader(http.StatusForbidden)
		return
	}
	grant.started = true
	c.mu.Unlock()
	n, err := io.CopyN(grant.writer, r.Body, grant.size)
	if err == nil && n != grant.size {
		err = io.ErrUnexpectedEOF
	}
	grant.done <- err
	if err != nil {
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func exactRange(values []string, size int64) (int64, int64, bool) {
	if len(values) != 1 || !strings.HasPrefix(values[0], "bytes=") {
		return 0, 0, false
	}
	parts := strings.Split(strings.TrimPrefix(values[0], "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	first, err1 := strconv.ParseInt(parts[0], 10, 64)
	last, err2 := strconv.ParseInt(parts[1], 10, 64)
	return first, last, err1 == nil && err2 == nil && first >= 0 && last >= first && last < size && last-first < channelChunk && parts[0] == strconv.FormatInt(first, 10) && parts[1] == strconv.FormatInt(last, 10)
}
