package plugins_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func TestRealWorkerUnlimitedDepthBeyond64(t *testing.T) {
	executable := buildWorker(t)
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	relative := strings.Repeat("d/", 70) + "deep.wav"
	file := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	wave(t, file)
	w, err := pluginhost.Start(context.Background(), pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", UserID: "owner", Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}, StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "limited", Generation: 1, Limit: 1})
	if items := scanAll(t, w, r); len(items) != 0 {
		t.Fatal("bounded scan included depth 70")
	}
	r.ScanID, r.Scope.MaxDepth, r.PlanDigest = "unlimited", 0, ""
	if items := scanAll(t, w, r); len(items) != 1 || items[0].RelativePath != relative {
		t.Fatal("unlimited scan did not include depth 70")
	}
}
