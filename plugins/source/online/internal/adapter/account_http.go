package adapter

import (
	"context"

	"io"
	"net/http"
	"net/url"
	"strings"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

func accountHost(host string) bool {
	switch host {
	case "interface.music.163.com", "ssl.ptlogin2.qq.com", "ssl.ptlogin2.graph.qq.com", "graph.qq.com", "u.y.qq.com", "api.music.apple.com":
		return true
	}
	return false
}

func (c *Client) accountHTTP(ctx context.Context, method, address, body string, headers map[string]string, cookies map[string]string) (data []byte, response *http.Response, requestErr error) {
	ctx = pluginlog.Context(ctx, pluginproto.OnlinePluginID(c.platform), c.scope.InstanceID)
	finish := pluginlog.Operation(ctx, "online.upstream.request")
	defer func() { finish(requestErr) }()
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || !accountHost(u.Host) || u.User != nil {
		return nil, nil, domain.Fail("ACCESS_DENIED", "Platform login destination is not allowed")
	}
	req, err := http.NewRequestWithContext(ctx, method, address, strings.NewReader(body))
	if err != nil {
		return nil, nil, upstreamInvalid()
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	addAccountCookies(req, cookies)
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		return nil, nil, domain.Fail("TEMPORARY_FAILURE", "Platform login request failed")
	}
	defer res.Body.Close()
	pluginlog.Logger(ctx).Log(ctx, pluginlog.LevelTrace, "Online upstream response received", "statusCode", res.StatusCode)
	if res.StatusCode != 200 && res.StatusCode != 302 {
		return nil, nil, c.statusError(res)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes {
		return nil, nil, upstreamInvalid()
	}
	return b, res, nil
}
