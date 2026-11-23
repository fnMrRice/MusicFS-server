package adapter

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/policies"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

const maxResponseBytes = 2 << 20

func publicTransport() *http.Transport {
	return &http.Transport{
		Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		DialContext: publicDial, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 1,
		IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 32 << 10,
	}
}

func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" || (host != "music.163.com" && host != "c.y.qq.com" && !accountHost(host) && !policies.NeteaseAudioHost(host)) {
		return nil, domain.Fail("ACCESS_DENIED", "Online destination is not allowed")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Online destination could not be resolved")
	}
	for _, addr := range addresses {
		if !publicAddress(addr) {
			return nil, domain.Fail("ACCESS_DENIED", "Online destination is not public")
		}
	}
	var last error
	for _, addr := range addresses {
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(addr.String(), port))
		if err == nil {
			return conn, nil
		}
		last = err
	}
	return nil, last
}

func publicAddress(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return false
	}
	for _, s := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16"} {
		if netip.MustParsePrefix(s).Contains(addr) {
			return false
		}
	}
	return true
}

func (c *Client) get(ctx context.Context, path string, query url.Values, dst any) error {
	if err := c.admit(ctx); err != nil {
		return err
	}
	if ctx.Value(accountContextKey{}) != c || c.account.revoked.Load() {
		return platformLoginRequired()
	}
	if c.platform != Netease {
		return domain.Fail("UNSUPPORTED", "No authenticated client adapter is available for this operation")
	}
	data := make(map[string]any, len(query))
	for key := range query {
		data[key] = query.Get(key)
	}
	return c.driver.Request(ctx, &c.account.secret, path, data, dst)
}

func (c *Client) statusError(res *http.Response) error {
	switch res.StatusCode {
	case http.StatusUnauthorized:
		return domain.Fail("AUTH_REQUIRED", "Online metadata requires authorization")
	case http.StatusForbidden:
		return domain.Fail("ACCESS_DENIED", "Online metadata access was denied")
	case http.StatusNotFound:
		return domain.Fail("NOT_FOUND", "Online metadata was not found")
	case http.StatusTooManyRequests:
		seconds := 60
		if n, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && n > 0 {
			seconds = min(n, 3600)
		} else if t, err := http.ParseTime(res.Header.Get("Retry-After")); err == nil {
			n := int(time.Until(t).Seconds()) + 1
			if n > 0 {
				seconds = min(n, 3600)
			}
		}
		return c.cooldown(seconds)
	default:
		return domain.Fail("TEMPORARY_FAILURE", "Online metadata is temporarily unavailable")
	}
}

func (c *Client) businessError(code int, ok int) error {
	if code == ok {
		return nil
	}
	switch code {
	case 301, 401:
		return domain.Fail("AUTH_REQUIRED", "Online metadata requires authorization")
	case 403:
		return domain.Fail("ACCESS_DENIED", "Online metadata access was denied")
	case 404:
		return domain.Fail("NOT_FOUND", "Online metadata was not found")
	case 429, 503:
		return c.cooldown(60)
	default:
		return domain.Fail("TEMPORARY_FAILURE", "Upstream metadata returned an unsuccessful response")
	}
}

func (c *Client) cooldown(seconds int) error {
	c.mu.Lock()
	if until := time.Now().Add(time.Duration(seconds) * time.Second); until.After(c.next) {
		c.next = until
	}
	c.mu.Unlock()
	return &domain.Error{Code: "RATE_LIMITED", Message: "Upstream metadata rate is limited", RetryAfter: &seconds}
}
