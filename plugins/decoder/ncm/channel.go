package ncmdecoder

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
)

type dataChannel struct {
	url    string
	server *http.Server
	stop   func() bool
}

func newDataChannel(ctx context.Context, nonce, purpose string, input io.ReaderAt, size int64) (*dataChannel, error) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("TEMPORARY_FAILURE: NCM channel unavailable")
	}
	host := listener.Addr().String()
	path := "/" + nonce + "/" + purpose
	slots := make(chan struct{}, 4)
	server := &http.Server{
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second,
		WriteTimeout: 5 * time.Second, IdleTimeout: 2 * time.Second, MaxHeaderBytes: 8192,
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != host || r.URL.EscapedPath() != path || r.URL.RawQuery != "" || r.URL.ForceQuery || r.Method != http.MethodGet || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if ctx.Err() != nil {
			w.WriteHeader(http.StatusGone)
			return
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		start, end, ok := exactRange(r.Header.Values("Range"), size)
		if !ok {
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		body := make([]byte, end-start+1)
		n, err := input.ReadAt(body, start)
		if n != len(body) || (err != nil && err != io.EOF) {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body)
	})
	limited := &channelListener{Listener: listener, slots: make(chan struct{}, 8), done: make(chan struct{})}
	channel := &dataChannel{url: "http://" + host + path, server: server}
	channel.stop = context.AfterFunc(ctx, func() { _ = server.Close() })
	go func() { _ = server.Serve(limited) }()
	return channel, nil
}

func (c *dataChannel) Close() {
	c.stop()
	_ = c.server.Close()
}

func exactRange(headers []string, size int64) (int64, int64, bool) {
	if len(headers) != 1 || !strings.HasPrefix(headers[0], "bytes=") {
		return 0, 0, false
	}
	pair := strings.Split(strings.TrimPrefix(headers[0], "bytes="), "-")
	if len(pair) != 2 {
		return 0, 0, false
	}
	start, e1 := strconv.ParseInt(pair[0], 10, 64)
	end, e2 := strconv.ParseInt(pair[1], 10, 64)
	return start, end, e1 == nil && e2 == nil && start >= 0 && end >= start && end < size && end-start < channelChunk && pair[0] == strconv.FormatInt(start, 10) && pair[1] == strconv.FormatInt(end, 10)
}

type channelListener struct {
	net.Listener
	slots chan struct{}
	done  chan struct{}
	once  sync.Once
}

func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case l.slots <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}
	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.slots
		return nil, err
	}
	return &channelConn{Conn: conn, release: func() { <-l.slots }}, nil
}

func (l *channelListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type channelConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *channelConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
