package plugins_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func ncmTestPeer(t *testing.T, executable, nonce string) (*pluginproto.Peer, context.Context) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	cmd := exec.CommandContext(ctx, executable)
	cmd.Dir = filepath.Dir(executable)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	peer := pluginproto.NewPeer(ctx, stdout, stdin, "h-", nil)
	t.Cleanup(func() { _ = peer.Close(); cancel(); _ = cmd.Wait() })
	var ready pluginproto.Ready
	hello := pluginproto.Hello{ProtocolVersion: "1.0", InstanceID: "f46385a2-e358-55e2-bde8-07b7808f3e32", TaskID: "task", Nonce: nonce, ConfigRevision: "1"}
	testkit.ValidatePlugin(t, "DecoderHello", hello)
	err = peer.Call(ctx, "host.hello", hello, &ready)
	if err != nil {
		t.Fatal(err)
	}
	testkit.ValidatePlugin(t, "Ready", ready)
	if ready.Manifest.PluginID != "musicfs.plugin.decoder.ncm" || ready.Nonce != nonce {
		t.Fatal("invalid NCM worker identity")
	}
	return peer, ctx
}

func TestActualNCMChannelValidation(t *testing.T) {
	worker, _ := buildNCMWorker(t)
	nonce := strings.Repeat("b", 48)
	// A valid MP3 frame prefix is sufficient for transport-specific checks.
	plain := append([]byte{0xff, 0xfb, 0x90, 0}, bytes.Repeat([]byte{0x55}, 150000)...)
	cover := bytes.Repeat([]byte{0x71}, 2048)
	encrypted, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{Cover: cover, CoverPadding: 4096})
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"valid", "full-response", "redirect", "wrong-range", "duplicate-range", "wrong-length", "short-body", "encoded", "wrong-nonce", "query", "localhost", "wrong-task", "extra-field"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var start, end int64
				if _, err := fmt.Sscanf(r.Header.Get("Range"), "bytes=%d-%d", &start, &end); err != nil || start < 0 || end < start || end >= int64(len(encrypted)) || end-start >= 65536 {
					w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
					return
				}
				body := encrypted[start : end+1]
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(encrypted)))
				w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				status := http.StatusPartialContent
				switch mode {
				case "full-response":
					status = http.StatusOK
				case "redirect":
					status = http.StatusTemporaryRedirect
					w.Header().Set("Location", "/redirected")
				case "wrong-range":
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(encrypted)+1))
				case "duplicate-range":
					w.Header().Add("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(encrypted)+1))
				case "wrong-length":
					w.Header().Set("Content-Length", fmt.Sprint(len(body)+1))
				case "short-body":
					body = body[:len(body)-1]
				case "encoded":
					w.Header().Set("Content-Encoding", "gzip")
				}
				w.WriteHeader(status)
				_, _ = w.Write(body)
			}))
			defer server.Close()
			peer, ctx := ncmTestPeer(t, worker, nonce)
			request := map[string]any{"taskId": "task", "assetId": "asset", "nonce": nonce, "inputUrl": server.URL + "/" + nonce + "/input", "size": len(encrypted)}
			switch mode {
			case "wrong-nonce":
				request["inputUrl"] = server.URL + "/" + strings.Repeat("a", 48) + "/input"
			case "query":
				request["inputUrl"] = request["inputUrl"].(string) + "?other=1"
			case "localhost":
				request["inputUrl"] = strings.Replace(request["inputUrl"].(string), "127.0.0.1", "localhost", 1)
			case "wrong-task":
				request["taskId"] = "another-task"
			case "extra-field":
				request["sourcePath"] = "private.ncm"
			}
			var result map[string]any
			err := peer.Call(ctx, "ncm.open", request, &result)
			if mode == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				testkit.ValidatePlugin(t, "NCMOpened", result)
				checkNCMOutputRequests(t, result["outputUrl"].(string), plain)
				artwork, ok := result["artwork"].(map[string]any)
				if !ok || artwork["size"] != float64(len(cover)) {
					t.Fatalf("invalid artwork descriptor: %+v", result["artwork"])
				}
				checkNCMOutputRequests(t, artwork["url"].(string), cover)
			} else if err == nil {
				t.Fatalf("unsafe channel accepted for mode %s", mode)
			}
			if mode == "redirect" && calls.Load() != 1 {
				t.Fatalf("redirect was followed: %d requests", calls.Load())
			}
			if (mode == "wrong-nonce" || mode == "query" || mode == "localhost" || mode == "wrong-task" || mode == "extra-field") && calls.Load() != 0 {
				t.Fatalf("invalid binding reached input server: %d", calls.Load())
			}
			if err := peer.Call(ctx, "ncm.open", request, nil); err == nil || !strings.HasPrefix(err.Error(), "CONFLICT") {
				t.Fatalf("second input binding was accepted: %v", err)
			}
			if mode == "valid" {
				_ = peer.Close()
				checkNCMChannelClosed(t, result["outputUrl"].(string))
				checkNCMChannelClosed(t, result["artwork"].(map[string]any)["url"].(string))
			}
		})
	}
}

func checkNCMOutputRequests(t *testing.T, rawURL string, plain []byte) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	defer client.CloseIdleConnections()
	for _, tc := range []struct {
		method, suffix, rangeHeader, host string
		status                            int
	}{
		{http.MethodGet, "", "bytes=255-511", "", http.StatusPartialContent},
		{http.MethodGet, "", "", "", http.StatusRequestedRangeNotSatisfiable},
		{http.MethodGet, "", "bytes=0-65536", "", http.StatusRequestedRangeNotSatisfiable},
		{http.MethodGet, "", "bytes=0-1,3-4", "", http.StatusRequestedRangeNotSatisfiable},
		{http.MethodGet, "", fmt.Sprintf("bytes=0-%d", len(plain)), "", http.StatusRequestedRangeNotSatisfiable},
		{http.MethodGet, "?other=1", "bytes=0-1", "", http.StatusForbidden},
		{http.MethodGet, "", "bytes=0-1", "other-host", http.StatusForbidden},
		{http.MethodHead, "", "bytes=0-1", "", http.StatusForbidden},
	} {
		req, err := http.NewRequest(tc.method, rawURL+tc.suffix, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Range", tc.rangeHeader)
		if tc.host != "" {
			req.Host = tc.host
		}
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil || response.StatusCode != tc.status {
			t.Fatalf("output validation failed: status=%d wanted=%d err=%v", response.StatusCode, tc.status, err)
		}
		if tc.status == http.StatusPartialContent && !bytes.Equal(body, plain[255:512]) {
			t.Fatal("output range content mismatch")
		}
	}
}

func checkNCMChannelClosed(t *testing.T, rawURL string) {
	t.Helper()
	client := &http.Client{Timeout: time.Second}
	defer client.CloseIdleConnections()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		request, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Range", "bytes=0-0")
		response, err := client.Do(request)
		if err != nil {
			return
		}
		_ = response.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("closed NCM worker left its data channel reachable")
}
