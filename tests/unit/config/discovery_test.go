package config_test

import (
	"net"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/config"
	"github.com/fnMrRice/MusicFS-server/src/core/discovery"
)

func TestLANDiscoveryConfiguration(t *testing.T) {
	doc, err := parse(t, minimalYAML)
	if err != nil || !doc.Config.Discovery.Enabled {
		t.Fatal("missing discovery defaults", err)
	}
	disabled, err := parse(t, minimalYAML+"discovery: {enabled: false}\n")
	if err != nil || disabled.Config.Discovery.Enabled {
		t.Fatal("discovery switch was ignored", err)
	}
	if _, err := parse(t, minimalYAML+"discovery: {origin: 'https://not-allowed.example'}\n"); err == nil {
		t.Fatal("accepted discovery origin outside allowedOrigins")
	}
	c := config.Config{AllowedOrigins: []string{
		"http://127.0.0.1:443", "https://localhost", "https://127.0.0.1", "https://[::1]",
		"https://[::]", "https://0.0.0.0", "https://music.example:8443", "https://a.example", "https://[fd00::1]",
	}}
	if got := c.DiscoveryOrigin(443); got != "https://a.example" {
		t.Fatal("unexpected default HTTPS origin", got)
	}
	c.Discovery.Origin = "https://[fd00::1]"
	if c.DiscoveryOrigin(443) != c.Discovery.Origin || c.DiscoveryOrigin(8443) != "" {
		t.Fatal("incorrect explicit IPv6 origin or port")
	}
	long := "https://" + strings.Repeat("abcdefghij.", 22) + "example"
	c.Discovery.Origin, c.AllowedOrigins = long, []string{long}
	if c.DiscoveryOrigin(443) != "" {
		t.Fatal("oversized DNS-SD TXT record")
	}
}

func TestDiscoverySkipsLocalOnlyListeners(t *testing.T) {
	for _, tc := range []struct {
		enabled bool
		cert    string
		ip      string
	}{{true, "cert", "127.0.0.1"}, {true, "cert", "::1"}, {true, "", "192.0.2.1"}, {false, "cert", "0.0.0.0"}} {
		c := config.Config{TLSCert: tc.cert, Discovery: config.DiscoveryConfig{Enabled: tc.enabled}}
		stop, err := discovery.Advertise(c, &net.TCPAddr{IP: net.ParseIP(tc.ip), Port: 8443}, "test")
		if err != nil || stop != nil {
			t.Fatalf("unexpected broadcast for %+v: %v", tc, err)
		}
	}
	c := config.Config{TLSCert: "cert", AllowedOrigins: []string{"https://127.0.0.1:8443"}, Discovery: config.DiscoveryConfig{Enabled: true}}
	stop, err := discovery.Advertise(c, &net.TCPAddr{IP: net.IPv4zero, Port: 8443}, "test")
	if err == nil || stop != nil {
		t.Fatal("LAN discovery published without an allowed LAN origin")
	}
}
