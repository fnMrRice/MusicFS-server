package storage_test

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type scraperProxyNetwork struct {
	client                      *http.Client
	httpURL, httpsURL, socksURL string
	mu                          sync.Mutex
	routes                      map[string]int
}

// 所有站点域名只映射到本机合成数据；代理仍通过真实 CONNECT/SOCKS5 握手转发 TLS 字节。
func newScraperProxyNetwork(t *testing.T, handler http.HandlerFunc) *scraperProxyNetwork {
	t.Helper()
	n := &scraperProxyNetwork{routes: map[string]int{}}
	upstream := httptest.NewTLSServer(handler)
	t.Cleanup(upstream.Close)
	upstreamAddress := upstream.Listener.Addr().String()
	tunnel := func(downstream net.Conn, reader io.Reader) {
		defer downstream.Close()
		up, err := net.DialTimeout("tcp", upstreamAddress, time.Second)
		if err != nil {
			t.Error(err)
			return
		}
		defer up.Close()
		done := make(chan struct{})
		go func() { _, _ = io.Copy(up, reader); _ = up.Close(); close(done) }()
		_, _ = io.Copy(downstream, up)
		_ = downstream.Close()
		<-done
	}
	proxy := func(route string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodConnect {
				http.Error(w, "CONNECT required", http.StatusMethodNotAllowed)
				return
			}
			n.record(route, r.Host)
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
			_ = rw.Flush()
			tunnel(conn, rw)
		}
	}
	httpProxy := httptest.NewServer(proxy("http"))
	httpsProxy := httptest.NewTLSServer(proxy("https"))
	t.Cleanup(httpProxy.Close)
	t.Cleanup(httpsProxy.Close)
	n.httpURL, n.httpsURL = httpProxy.URL, httpsProxy.URL
	socks, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = socks.Close() })
	n.socksURL = "socks5://" + socks.Addr().String()
	go func() {
		for {
			conn, err := socks.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				greeting := make([]byte, 2)
				if _, err := io.ReadFull(conn, greeting); err != nil || greeting[0] != 5 {
					return
				}
				if _, err := io.CopyN(io.Discard, conn, int64(greeting[1])); err != nil {
					return
				}
				_, _ = conn.Write([]byte{5, 0})
				header := make([]byte, 4)
				if _, err := io.ReadFull(conn, header); err != nil || header[0] != 5 || header[1] != 1 {
					return
				}
				length := 0
				switch header[3] {
				case 1:
					length = 4
				case 4:
					length = 16
				case 3:
					b := make([]byte, 1)
					if _, err := io.ReadFull(conn, b); err != nil {
						return
					}
					length = int(b[0])
				default:
					return
				}
				address := make([]byte, length+2)
				if _, err := io.ReadFull(conn, address); err != nil {
					return
				}
				host := string(address[:length])
				if header[3] != 3 {
					host = net.IP(address[:length]).String()
				}
				n.record("socks", net.JoinHostPort(host, fmt.Sprint(binary.BigEndian.Uint16(address[length:]))))
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				tunnel(conn, conn)
			}()
		}
	}()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == httpProxy.Listener.Addr().String() || address == httpsProxy.Listener.Addr().String() || address == socks.Addr().String() {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}
		n.record("direct", address)
		return (&net.Dialer{}).DialContext(ctx, network, upstreamAddress)
	}
	n.client = &http.Client{Transport: transport, Timeout: 10 * time.Second}
	t.Cleanup(n.client.CloseIdleConnections)
	return n
}

func (n *scraperProxyNetwork) record(route, host string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.routes[route+"/"+host]++
}

func (n *scraperProxyNetwork) count(route, host string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.routes[route+"/"+host]
}

func (n *scraperProxyNetwork) socksHURL() string {
	return strings.Replace(n.socksURL, "socks5:", "socks5h:", 1)
}
