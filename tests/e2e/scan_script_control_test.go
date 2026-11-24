package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestScanScriptWaitsAcrossIdleConnectionAndReconnect(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("PowerShell is unavailable")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var submitted, connected, reads atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/session", func(w http.ResponseWriter, _ *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "musicfs_session", Value: "fixture-session", Path: "/"})
		fmt.Fprint(w, `{"csrfToken":"fixture-csrf-token"}`)
	})
	mux.HandleFunc("DELETE /api/v1/auth/session", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("GET /api/v1/provider-instances", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"items":[{"id":"local","roots":[{"id":"music"}],"configuration":{"schemaRevision":"revision"}}]}`)
	})
	mux.HandleFunc("POST /api/v1/provider-instances/local/scan-jobs", func(w http.ResponseWriter, _ *http.Request) {
		submitted.Add(1)
		fmt.Fprint(w, `{"jobId":"test-scan"}`)
	})
	mux.HandleFunc("GET /api/v1/scan-jobs/test-scan", func(w http.ResponseWriter, _ *http.Request) {
		status := "running"
		if reads.Add(1) == 3 {
			status = "succeeded"
		}
		fmt.Fprintf(w, `{"jobId":"test-scan","status":%q}`, status)
	})
	mux.HandleFunc("GET /api/v1/control", func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("musicfs_session")
		if err != nil || cookie.Value != "fixture-session" || r.Header.Get("Origin") != "http://"+r.Host {
			t.Error("missing handshake credentials or origin")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"musicfs.control.v1"}})
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.CloseNow()
		var auth struct{ Type, CSRFToken string }
		if err := wsjson.Read(ctx, conn, &auth); err != nil || auth.Type != "auth" || auth.CSRFToken != "fixture-csrf-token" {
			t.Error("invalid first frame", err)
			return
		}
		send := func(message string) bool {
			err := conn.Write(ctx, websocket.MessageText, []byte(message))
			if err != nil {
				t.Error(err)
			}
			return err == nil
		}
		if !send(`{"type":"ready","heartbeatIntervalMs":120,"heartbeatTimeoutMs":1500}`) {
			return
		}
		if connected.Add(1) == 1 {
			if !send(`{"type":"event","event":"activity.changed","data":{"resources":["pluginJobs"]}}`) {
				return
			}
			// 两次心跳之间没有扫描变化，客户端必须保活且不能反复读取任务。
			for range 2 {
				var ping struct{ Type string }
				if err := wsjson.Read(ctx, conn, &ping); err != nil || ping.Type != "ping" {
					t.Error("missing heartbeat", err)
					return
				}
				if !send(`{"type":"pong"}`) {
					return
				}
			}
			if reads.Load() != 1 {
				t.Errorf("idle connection read the job %d times", reads.Load())
			}
			return
		}
		if send(`{"type":"event","event":"activity.changed","data":{"resources":["scans"]}}`) {
			<-conn.CloseRead(ctx).Done()
		}
	})
	host := httptest.NewServer(mux)
	defer host.Close()
	cmd := exec.CommandContext(ctx, pwsh, "-NoProfile", "-File", filepath.Join("..", "..", "scripts", "scan.ps1"), "-BaseURL", host.URL)
	cmd.Env = append(os.Environ(), "MUSICFS_ADMIN_TOKEN=test-token")
	output, err := cmd.CombinedOutput()
	var job struct{ JobID, Status string }
	if err != nil || json.Unmarshal(output, &job) != nil || job.JobID != "test-scan" || job.Status != "succeeded" {
		t.Fatalf("script failed: %v %s", err, output)
	}
	if submitted.Load() != 1 || connected.Load() != 2 || reads.Load() != 3 {
		t.Fatalf("unexpected submits=%d connects=%d reads=%d", submitted.Load(), connected.Load(), reads.Load())
	}
}
