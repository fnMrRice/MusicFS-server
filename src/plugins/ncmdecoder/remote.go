package ncmdecoder

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type channelReader struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64
	slots  chan struct{}
}

func newChannelReader(ctx context.Context, rawURL, nonce, purpose string, size int64) (*channelReader, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.EscapedPath() != "/"+nonce+"/"+purpose || !validNonce(nonce) || size <= 0 {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid NCM channel")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 || u.Host != "127.0.0.1:"+strconv.Itoa(port) {
		return nil, fmt.Errorf("ACCESS_DENIED: invalid NCM channel port")
	}
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	transport := &http.Transport{
		Proxy: nil, DisableCompression: true, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4,
		ResponseHeaderTimeout: 3 * time.Second, IdleConnTimeout: 2 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != u.Host {
				return nil, fmt.Errorf("ACCESS_DENIED: NCM channel address changed")
			}
			return dialer.DialContext(ctx, "tcp4", address)
		},
	}
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return fmt.Errorf("ACCESS_DENIED: NCM channel redirect refused")
	}}
	return &channelReader{ctx: ctx, client: client, url: rawURL, size: size, slots: make(chan struct{}, 4)}, nil
}

func (r *channelReader) Close() { r.client.CloseIdleConnections() }

func (r *channelReader) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("ACCESS_DENIED: negative NCM channel offset")
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if offset >= r.size {
		return 0, io.EOF
	}
	select {
	case r.slots <- struct{}{}:
		defer func() { <-r.slots }()
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	}
	length := len(dst)
	if int64(length) > r.size-offset {
		length = int(r.size - offset)
	}
	read := 0
	for read < length {
		count := min(channelChunk, length-read)
		n, err := r.readChunk(dst[read:read+count], offset+int64(read))
		read += n
		if err != nil {
			return read, err
		}
	}
	if read != len(dst) {
		return read, io.EOF
	}
	return read, nil
}

func (r *channelReader) readChunk(dst []byte, offset int64) (int, error) {
	request, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
	if err != nil {
		return 0, fmt.Errorf("ACCESS_DENIED: invalid NCM channel request")
	}
	end := offset + int64(len(dst)) - 1
	request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", offset, end))
	response, err := r.client.Do(request)
	if err != nil {
		if r.ctx.Err() != nil {
			return 0, r.ctx.Err()
		}
		return 0, fmt.Errorf("TEMPORARY_FAILURE: NCM channel request failed")
	}
	defer response.Body.Close()
	expected := fmt.Sprintf("bytes %d-%d/%d", offset, end, r.size)
	if response.StatusCode != http.StatusPartialContent || len(response.Header.Values("Content-Range")) != 1 || response.Header.Get("Content-Range") != expected || response.ContentLength != int64(len(dst)) || len(response.TransferEncoding) != 0 || len(response.Header.Values("Content-Encoding")) != 0 {
		return 0, fmt.Errorf("PROTOCOL_MISMATCH: invalid NCM channel response")
	}
	n, err := io.ReadFull(response.Body, dst)
	if err != nil {
		return n, fmt.Errorf("RESOURCE_CHANGED: truncated NCM channel response: %w", io.ErrUnexpectedEOF)
	}
	var extra [1]byte
	if n, err := response.Body.Read(extra[:]); n != 0 || err != io.EOF {
		return 0, fmt.Errorf("PROTOCOL_MISMATCH: excessive NCM channel response")
	}
	return len(dst), nil
}
