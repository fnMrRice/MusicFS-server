package scraperlocal

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func localClient(host string) *http.Client {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil, DisableCompression: true, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4, ResponseHeaderTimeout: 3 * time.Second, IdleConnTimeout: 2 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if address != host {
				return nil, fmt.Errorf("ACCESS_DENIED: scraper channel address changed")
			}
			return dialer.DialContext(ctx, "tcp4", address)
		}},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return fmt.Errorf("ACCESS_DENIED: scraper channel redirect refused")
		}}
}

type remoteReader struct {
	ctx    context.Context
	client *http.Client
	url    string
	size   int64
}

func (r *remoteReader) ReadAt(dst []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("ACCESS_DENIED: negative resource offset")
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
	length := min(int64(len(dst)), r.size-offset)
	done := 0
	for int64(done) < length {
		count := int(min(int64(channelChunk), length-int64(done)))
		position := offset + int64(done)
		end := position + int64(count) - 1
		request, err := http.NewRequestWithContext(r.ctx, http.MethodGet, r.url, nil)
		if err != nil {
			return done, fmt.Errorf("ACCESS_DENIED: invalid resource request")
		}
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", position, end))
		response, err := r.client.Do(request)
		if err != nil {
			if r.ctx.Err() != nil {
				return done, r.ctx.Err()
			}
			return done, fmt.Errorf("TEMPORARY_FAILURE: scraper resource unavailable")
		}
		if response.StatusCode != http.StatusPartialContent || response.ContentLength != int64(count) || len(response.Header.Values("Content-Range")) != 1 || response.Header.Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", position, end, r.size) || len(response.TransferEncoding) != 0 || len(response.Header.Values("Content-Encoding")) != 0 {
			response.Body.Close()
			return done, fmt.Errorf("RESOURCE_CHANGED: invalid scraper resource response")
		}
		n, err := io.ReadFull(response.Body, dst[done:done+count])
		var extra [1]byte
		trailing, endErr := response.Body.Read(extra[:])
		response.Body.Close()
		done += n
		if err != nil || trailing != 0 || endErr != io.EOF {
			return done, fmt.Errorf("RESOURCE_CHANGED: truncated scraper resource")
		}
	}
	if done != len(dst) {
		return done, io.EOF
	}
	return done, nil
}
