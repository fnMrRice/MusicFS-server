package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
)

func TestWorkerHelperProcess(t *testing.T) {
	mode := ""
	for _, arg := range os.Args {
		if strings.HasPrefix(arg, "musicfs-helper=") {
			mode = strings.TrimPrefix(arg, "musicfs-helper=")
		}
	}
	if mode == "" {
		return
	}
	if mode == "hang" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	if mode == "truncate" {
		fmt.Fprint(os.Stdout, "Content-Length: 100\r\n\r\n{}")
		os.Exit(0)
	}
	if mode == "crash" {
		os.Exit(23)
	}
	framer := pluginproto.NewFramer(os.Stdin, os.Stdout)
	request, err := framer.Read()
	if err != nil {
		os.Exit(2)
	}
	var hello pluginproto.Hello
	json.Unmarshal(request.Params, &hello)
	ready := pluginproto.Ready{InstanceID: hello.InstanceID, Nonce: hello.Nonce, Manifest: pluginproto.Manifest{PluginID: "musicfs.plugin.source.localfiles", PluginVersion: "0.1.0", ProtocolVersion: "9.0", Roles: []string{"provider"}, MaxMessageBytes: pluginproto.MaxMessageBytes, MaxPageItems: pluginproto.MaxPageItems}}
	result, _ := json.Marshal(ready)
	framer.Write(pluginproto.Envelope{ID: request.ID, Result: result})
	time.Sleep(time.Minute)
	os.Exit(0)
}

func TestRealWorkerFailuresHaveBoundedCleanup(t *testing.T) {
	b, err := os.ReadFile(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	for _, mode := range []string{"hang", "truncate", "crash", "wrong-version"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancel()
			start := time.Now()
			cfg := pluginhost.Config{Executable: os.Args[0], Args: []string{"-test.run=TestWorkerHelperProcess", "--", "musicfs-helper=" + mode}, SHA256: fmt.Sprintf("%x", sum), InstanceID: "76ac344e-a6c3-5343-a04f-56dbe4f03c8b", UserID: "owner", StateDir: t.TempDir()}
			if w, err := pluginhost.Start(ctx, cfg); err == nil {
				w.Close()
				t.Fatal("broken worker accepted")
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("failed worker cleanup exceeded deadline")
			}
		})
	}
}
