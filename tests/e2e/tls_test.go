package e2e_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/auth"
	"github.com/fnMrRice/MusicFS-server/src/bootstrap"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestActualTLSCookieCSRFRangeAndRevocation(t *testing.T) {
	cfg, plainBase := yamlServiceConfig(t)
	base := strings.Replace(plainBase, "http:", "https:", 1)
	cfg.AllowLoopbackHTTP = false
	cfg.AllowedOrigins = []string{base}
	pool := tlsFixture(t, &cfg)
	music := t.TempDir()
	payload, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(music, "tls.wav"), payload, 0600); err != nil {
		t.Fatal(err)
	}
	cfg.Plugins = []config.Plugin{{PluginID: "musicfs.plugin.source.localfiles", Executable: build(t, "musicfs-plugin-local"), Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Synthetic", Path: music}}}}}}}
	loaded, err := config.Load(writeYAMLConfiguration(t, cfg), map[string]string{"ADMIN_TOKEN": cfg.AdminToken})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- bootstrap.Serve(ctx, loaded, slog.New(slog.NewTextHandler(io.Discard, nil))) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(15 * time.Second):
			t.Error("TLS server shutdown timed out")
		}
	})
	jar, _ := cookiejar.New(nil)
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	defer transport.CloseIdleConnections()
	c := &client{http: &http.Client{Jar: jar, Transport: transport, Timeout: 10 * time.Second}, base: base}
	for deadline := time.Now().Add(15 * time.Second); ; {
		response, err := c.http.Get(base + "/api/v1/health/ready")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("verified TLS readiness timed out", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	login, _ := http.NewRequest("POST", base+"/api/v1/auth/session", nil)
	login.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	login.Header.Set("Origin", base)
	response, err := c.http.Do(login)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(response.Body)
	response.Body.Close()
	var session auth.Session
	if err != nil || response.StatusCode != 200 || json.Unmarshal(raw, &session) != nil {
		t.Fatalf("TLS login failed: %d", response.StatusCode)
	}
	c.csrf = session.CSRFToken
	cookies := response.Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode {
		t.Fatal("TLS cookie protection missing")
	}
	for _, headers := range []map[string]string{
		{"Origin": "https://untrusted.invalid", "X-CSRF-Token": c.csrf, "X-Forwarded-Proto": "https", "X-Forwarded-Host": strings.TrimPrefix(base, "https://")},
		{"Origin": base},
	} {
		r, _ := http.NewRequest("DELETE", base+"/api/v1/auth/session", nil)
		for name, value := range headers {
			r.Header.Set(name, value)
		}
		response, err = c.http.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatal("TLS mutation accepted without actual allowed Origin and CSRF")
		}
	}
	track := scanNCM(t, c)["tls.wav"]
	code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", playback.CreateRequest{SourceTrackID: track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}, "tls-playback-check")
	var d playback.Description
	if code != 201 || json.Unmarshal(raw, &d) != nil {
		t.Fatalf("TLS playback: %d %s", code, raw)
	}
	verifyRanges(t, c, d, payload)
	code, _, _ = c.request(t, "DELETE", "/api/v1/auth/session", nil, "")
	if code != 204 {
		t.Fatal("TLS logout failed")
	}
	r, _ := http.NewRequest("GET", base+d.StreamURL, nil)
	r.AddCookie(cookies[0])
	response, err = c.http.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatalf("revoked TLS cookie remained readable: %d", response.StatusCode)
	}
	got, err := os.ReadFile(filepath.Join(music, "tls.wav"))
	if err != nil || string(got) != string(payload) {
		t.Fatal("TLS playback changed music")
	}
}

func tlsFixture(t *testing.T, cfg *config.Config) *x509.CertPool {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "MusicFS isolated TLS test"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	cfg.TLSCert = filepath.Join(root, "server.crt")
	cfg.TLSKey = filepath.Join(root, "server.key")
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err = os.WriteFile(cfg.TLSCert, cert, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(cfg.TLSKey, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		t.Fatal("TLS fixture certificate invalid")
	}
	return pool
}
