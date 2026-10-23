package bilibili

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

const apiRoot = "https://api.bilibili.com"
const audioRoot = "https://www.bilibili.com/audio/music-service-c/web"

func allowedHost(host string) bool {
	return host == "api.bilibili.com" || host == "www.bilibili.com" || cdnHost(host)
}

func cdnHost(host string) bool {
	return strings.HasSuffix(host, ".bilivideo.com") || strings.HasSuffix(host, ".bilivideo.cn") || strings.HasSuffix(host, ".bilivideo.net") || host == "upos-hz-mirrorakam.akamaized.net"
}

func safeCDN(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) <= 8192 && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Fragment == "" && cdnHost(u.Hostname())
}

func publicTransport() *http.Transport {
	return &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: publicDial, MaxConnsPerHost: 2, MaxIdleConnsPerHost: 1, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 32 << 10}
}

func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" || !allowedHost(host) {
		return nil, fault.Fail("ACCESS_DENIED", "Bilibili destination is not allowed")
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(addresses) == 0 {
		return nil, fault.Fail("TEMPORARY_FAILURE", "Bilibili destination could not be resolved")
	}
	for _, address := range addresses {
		if !publicAddress(address) {
			return nil, fault.Fail("ACCESS_DENIED", "Bilibili destination is not public")
		}
	}
	for _, address := range addresses {
		conn, dialErr := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		err = dialErr
	}
	return nil, err
}

func publicAddress(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "64:ff9b::/96", "64:ff9b:1::/48", "2002::/16"} {
		if netip.MustParsePrefix(raw).Contains(address) {
			return false
		}
	}
	return true
}

func (c *Client) get(ctx context.Context, endpoint string, query url.Values, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+query.Encode(), nil)
	if err != nil {
		return online.UpstreamInvalid()
	}
	req.Header.Set("Referer", "https://www.bilibili.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 MusicFS/0.1")
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fault.Fail("TEMPORARY_FAILURE", "Bilibili request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return statusError(res)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil || len(body) > 2<<20 {
		return online.UpstreamInvalid()
	}
	var envelope struct {
		Code *int            `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Code == nil {
		return online.UpstreamInvalid()
	}
	if err := businessError(*envelope.Code); err != nil {
		return err
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" || json.Unmarshal(envelope.Data, dst) != nil {
		return online.UpstreamInvalid()
	}
	return nil
}

func statusError(res *http.Response) error {
	switch res.StatusCode {
	case 401:
		return fault.Fail("PLATFORM_AUTH_REQUIRED", "Bilibili requires account authorization")
	case 403:
		return fault.Fail("ACCESS_DENIED", "Bilibili denied access to this resource")
	case 404, 410:
		return fault.Fail("NOT_FOUND", "Bilibili resource was not found")
	case 412:
		return fault.Fail("RISK_CONTROL", "Bilibili risk control rejected the request")
	case 429:
		retry := 60
		if value, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && value > 0 {
			retry = min(value, 3600)
		}
		return &fault.Error{Code: "RATE_LIMITED", Message: "Bilibili request rate is limited", RetryAfter: &retry}
	default:
		return fault.Fail("TEMPORARY_FAILURE", "Bilibili is temporarily unavailable")
	}
}

func businessError(code int) error {
	switch code {
	case 0:
		return nil
	case -101:
		return fault.Fail("PLATFORM_AUTH_REQUIRED", "Bilibili requires account authorization")
	case -403:
		return fault.Fail("ACCESS_DENIED", "Bilibili denied access to this resource")
	case -404, 62002, 62004, 7201006, 72010027:
		return fault.Fail("NOT_FOUND", "Bilibili resource was not found")
	case -352, -412, -401:
		return fault.Fail("RISK_CONTROL", "Bilibili risk control rejected the request")
	case -509, 429:
		return fault.Fail("RATE_LIMITED", "Bilibili request rate is limited")
	default:
		return fault.Fail("TEMPORARY_FAILURE", "Bilibili returned an unsuccessful response")
	}
}
