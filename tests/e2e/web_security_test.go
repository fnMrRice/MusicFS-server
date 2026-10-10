package e2e_test

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestWebPolicyAllowsLoginQRImagesAndPlatformAvatars(t *testing.T) {
	cfg, base := yamlServiceConfig(t)
	cfg.WebRoot = t.TempDir()
	const page = "<!doctype html><title>Login QR policy fixture</title>"
	if err := os.WriteFile(filepath.Join(cfg.WebRoot, "index.html"), []byte(page), 0600); err != nil {
		t.Fatal(err)
	}
	startNCMServer(t, cfg, base)
	c := &client{http: &http.Client{}, base: base}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		t.Run(method, func(t *testing.T) {
			code, body, header := c.request(t, method, "/", nil, "")
			if code != http.StatusOK || (method == http.MethodGet && string(body) != page) {
				t.Fatalf("Web artifact was not served: %d", code)
			}
			directives := map[string][]string{}
			for directive := range strings.SplitSeq(header.Get("Content-Security-Policy"), ";") {
				fields := strings.Fields(directive)
				if len(fields) > 0 {
					directives[fields[0]] = fields[1:]
				}
			}
			if !slices.Equal(directives["img-src"], []string{"'self'", "data:", "https://p1.music.126.net", "https://p2.music.126.net", "https://p3.music.126.net", "https://p4.music.126.net"}) {
				t.Fatal("Web policy must allow same-origin images, inline QR images and verified platform avatar hosts")
			}
			if !slices.Equal(directives["default-src"], []string{"'self'"}) || !slices.Equal(directives["object-src"], []string{"'none'"}) {
				t.Fatal("Login QR images must not relax other resource policies")
			}
			if header.Get("X-Content-Type-Options") != "nosniff" {
				t.Fatal("Web artifact MIME protection is missing")
			}
		})
	}
}
