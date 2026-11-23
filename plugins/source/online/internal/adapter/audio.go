package adapter

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"hash"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"
	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type audioDriver interface {
	AudioResource(context.Context, string) (pluginproto.OnlineAudio, string, error)
}

type audioResource struct {
	description pluginproto.OnlineAudio
	url         string
}

func (c *Client) audioResource(ctx context.Context, id string) (audioResource, error) {
	driver, ok := c.driver.(audioDriver)
	if !ok {
		return audioResource{}, fault.Fail("UNSUPPORTED", "Online audio is unavailable")
	}
	metadata, address, err := driver.AudioResource(ctx, id)
	if err != nil {
		return audioResource{}, err
	}
	address, err = audioURL(address)
	return audioResource{metadata, address}, err
}

func audioURL(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || len(address) > 8192 || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host != u.Hostname() || u.Fragment != "" || !policies.NeteaseAudioHost(strings.ToLower(u.Host)) {
		return "", fault.Fail("ACCESS_DENIED", "Online audio destination is not allowed")
	}
	u.Scheme = "https"
	return u.String(), nil
}

func (c *Client) DescribeAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, error) {
	resource, err := authorized(ctx, c, func(ctx context.Context) (audioResource, error) {
		return c.audioResource(ctx, id)
	})
	return resource.description, err
}

type mediaLifetime struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
}

func (m *mediaLifetime) revoke() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.ctx, m.cancel = nil, nil
	}
}

func (m *mediaLifetime) bind(parent context.Context) (context.Context, func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		m.ctx, m.cancel = context.WithCancel(context.Background())
	}
	ctx, cancel := context.WithCancel(m.ctx)
	stop := context.AfterFunc(parent, cancel)
	if parent.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}

type mediaBody struct {
	io.ReadCloser
	ctx     context.Context
	release func()
	digest  hash.Hash
	wantMD5 string
}

func (b *mediaBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.ReadCloser.Read(p)
	if b.digest != nil {
		b.digest.Write(p[:n])
		if err == io.EOF && hex.EncodeToString(b.digest.Sum(nil)) != b.wantMD5 {
			return n, fault.Fail("RESOURCE_CHANGED", "NetEase audio does not match its media version")
		}
	}
	return n, err
}

func (b *mediaBody) Close() error {
	b.release()
	return b.ReadCloser.Close()
}

func (c *Client) OpenAudio(ctx context.Context, id string) (pluginproto.OnlineAudio, io.ReadCloser, error) {
	var empty pluginproto.OnlineAudio
	var streamCtx context.Context
	var release func()
	resource, err := authorized(ctx, c, func(authCtx context.Context) (audioResource, error) {
		result, err := c.audioResource(authCtx, id)
		if err == nil {
			streamCtx, release = c.account.media.bind(ctx)
		}
		return result, err
	})
	if err != nil {
		if release != nil {
			release()
		}
		return empty, nil, err
	}
	body, err := c.openMedia(streamCtx, resource)
	if err != nil {
		release()
		return empty, nil, err
	}
	stream := &mediaBody{ReadCloser: body, ctx: streamCtx, release: release}
	if c.platform == Netease && resource.description.Version != "" {
		// 平台使用 MD5 标记媒体版本。只在完整接收时核对，以免错误副本进入缓存。
		stream.digest, stream.wantMD5 = md5.New(), resource.description.Version
	}
	return resource.description, stream, nil
}

func (c *Client) openMedia(ctx context.Context, resource audioResource) (io.ReadCloser, error) {
	// CDN 只接收播放 URL，不继承账号接口的 Cookie、请求头或超时。
	client := *c.http
	client.Timeout = 0
	address := resource.url
	for range 4 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return nil, upstreamInvalid()
		}
		req.Header.Set("Accept-Encoding", "identity")
		res, err := client.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fault.Fail("TEMPORARY_FAILURE", "Online audio download failed")
		}
		if res.StatusCode == http.StatusOK {
			if res.Header.Get("Content-Encoding") != "" || res.Header.Get("Content-Range") != "" || (res.ContentLength >= 0 && res.ContentLength != resource.description.SizeBytes) {
				res.Body.Close()
				return nil, fault.Fail("RESOURCE_CHANGED", "Online audio length or encoding changed")
			}
			contentType, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
			if strings.HasPrefix(contentType, "text/") || contentType == "application/json" || contentType == "application/xml" {
				res.Body.Close()
				return nil, fault.Fail("RESOURCE_CHANGED", "CDN returned a document instead of audio")
			}
			return res.Body, nil
		}
		res.Body.Close()
		switch res.StatusCode {
		case 301, 302, 303, 307, 308:
			location, err := res.Location()
			if err != nil {
				return nil, upstreamInvalid()
			}
			address, err = audioURL(location.String())
			if err != nil {
				return nil, err
			}
		default:
			return nil, c.statusError(res)
		}
	}
	return nil, fault.Fail("ACCESS_DENIED", "Online audio redirect limit exceeded")
}
