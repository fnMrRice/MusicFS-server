package config

import (
	"errors"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

type DiscoveryConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Origin  string `yaml:"origin,omitempty" json:"origin,omitempty"`
}

// DiscoveryOrigin 返回客户端实际用来登录的 HTTPS Origin，端口必须与 SRV 相同。
func (c Config) DiscoveryOrigin(port int) string {
	candidates := c.AllowedOrigins
	if c.Discovery.Origin != "" {
		if !slices.Contains(candidates, c.Discovery.Origin) {
			return ""
		}
		candidates = []string{c.Discovery.Origin}
	}
	for _, origin := range candidates {
		u, err := url.Parse(origin)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
			u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || len("origin="+origin) > 255 {
			continue
		}
		host := u.Hostname()
		fqdn := strings.TrimSuffix(strings.ToLower(host), ".")
		if fqdn == "localhost" || strings.HasSuffix(fqdn, ".localhost") || strings.Contains(host, "%") {
			continue
		}
		if ip, err := netip.ParseAddr(host); err == nil {
			if !ip.IsGlobalUnicast() || ip.IsLoopback() {
				continue
			}
		}
		servicePort := 443
		if u.Port() != "" {
			servicePort, err = strconv.Atoi(u.Port())
			if err != nil {
				continue
			}
		}
		if servicePort == port {
			return origin
		}
	}
	return ""
}

func (c Config) validateDiscovery(port int) error {
	if c.Discovery.Origin != "" && c.DiscoveryOrigin(port) == "" {
		return errors.New("discovery.origin must be a non-loopback HTTPS origin in allowedOrigins on the listener port, fitting one DNS-SD TXT entry")
	}
	return nil
}
