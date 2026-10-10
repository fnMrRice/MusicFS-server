package plugins_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
)

func scopedRequest(w *pluginhost.Worker, request pluginproto.ScanRequest) pluginproto.ScanRequest {
	if request.Scope.RootID == "" {
		for _, root := range w.Access.Roots() {
			if root.ID == request.RootID {
				request.Scope = pluginproto.ScanScope{RootID: root.ID, RootIdentity: root.ExpectedIdentity, Recursive: true, MaxDepth: 64}
			}
		}
	}
	if request.PlanDigest == "" {
		raw, _ := json.Marshal(request.Scope)
		request.PlanDigest = fmt.Sprintf("%x", sha256.Sum256(raw))
	}
	return request
}

func scanAll(t *testing.T, w *pluginhost.Worker, request pluginproto.ScanRequest) []pluginproto.Observation {
	t.Helper()
	request = scopedRequest(w, request)
	result := []pluginproto.Observation{}
	for {
		page, err := w.Scan(context.Background(), request)
		if err != nil {
			t.Fatal(err)
		}
		if page.PlanDigest != request.PlanDigest || len(page.Issues) > 0 {
			t.Fatal("scan lost its scope or completeness")
		}
		result = append(result, page.Items...)
		if page.NextCursor == "" {
			if !page.Complete {
				t.Fatal("incomplete source scan")
			}
			return result
		}
		request.Cursor = page.NextCursor
	}
}

func TestRealWorkerScopedDepthAndHardlinkIdentity(t *testing.T) {
	executable := buildWorker(t)
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "one", "two"), 0700); err != nil {
		t.Fatal(err)
	}
	wave(t, filepath.Join(root, "first.wav"))
	wave(t, filepath.Join(root, "one", "nested.wav"))
	wave(t, filepath.Join(root, "one", "two", "deep.wav"))
	cfg := pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), InstanceID: "local", UserID: "owner", Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}, StateDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w, err := pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { w.Close() }()
	request := scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "deep", Generation: 1, Limit: 1})
	deep := scanAll(t, w, request)
	if len(deep) != 3 {
		t.Fatalf("deep scan returned %d files", len(deep))
	}
	ids := map[string]string{}
	for _, item := range deep {
		ids[item.RelativePath] = item.ExternalID
	}
	shallow := request
	shallow.ScanID = "shallow"
	shallow.Scope.Recursive = false
	shallow.Scope.MaxDepth = 0
	shallow.PlanDigest = ""
	items := scanAll(t, w, shallow)
	if len(items) != 1 || items[0].ExternalID != ids["first.wav"] {
		t.Fatal("shallow scan changed scope or source identity")
	}
	one := request
	one.ScanID = "one-level"
	one.Scope.MaxDepth = 1
	one.PlanDigest = ""
	if items := scanAll(t, w, one); len(items) != 2 {
		t.Fatalf("depth one returned %d files", len(items))
	}
	if err := os.Link(filepath.Join(root, "first.wav"), filepath.Join(root, "one", "alias.wav")); err != nil {
		t.Fatal(err)
	}
	request.ScanID = "with-alias"
	withAlias := scanAll(t, w, request)
	if len(withAlias) != 4 {
		t.Fatal("hardlink provenance missing")
	}
	aliasID := ""
	for _, item := range withAlias {
		if known := ids[item.RelativePath]; known != "" && known != item.ExternalID {
			t.Fatal("adding hardlink changed an existing source ID")
		}
		if item.RelativePath == "one/alias.wav" {
			aliasID = item.ExternalID
		}
	}
	if aliasID == "" || aliasID == ids["first.wav"] {
		t.Fatal("hardlinks silently collapsed")
	}
	shallow.ScanID = "shallow-again"
	if items := scanAll(t, w, shallow); len(items) != 1 || items[0].ExternalID != ids["first.wav"] {
		t.Fatal("scoped scan reidentified existing hardlink")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = pluginhost.Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	request = scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "restart", Generation: 2, Limit: 1})
	for _, item := range scanAll(t, w, request) {
		if item.RelativePath == "first.wav" && item.ExternalID != ids["first.wav"] {
			t.Fatal("restart lost first alias")
		}
		if item.RelativePath == "one/alias.wav" && item.ExternalID != aliasID {
			t.Fatal("restart lost second alias")
		}
	}
	if err := os.Remove(filepath.Join(root, "one", "alias.wav")); err != nil {
		t.Fatal(err)
	}
	request.ScanID = "alias-removed"
	for _, item := range scanAll(t, w, request) {
		if item.RelativePath == "first.wav" && item.ExternalID != ids["first.wav"] {
			t.Fatal("removing sibling alias changed source ID")
		}
	}
}

func TestRealWorkerCursorBindsTaskGenerationPlanAndDepth(t *testing.T) {
	executable := buildWorker(t)
	body, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	wave(t, filepath.Join(root, "a.wav"))
	wave(t, filepath.Join(root, "b.wav"))
	w, err := pluginhost.Start(context.Background(), pluginhost.Config{Executable: executable, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), InstanceID: "local", UserID: "owner", Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}, StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	r := scopedRequest(w, pluginproto.ScanRequest{RootID: "music", ScanID: "task", Generation: 1, Limit: 1})
	first, err := w.Scan(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	r.Cursor = first.NextCursor
	for _, mutate := range []func(*pluginproto.ScanRequest){
		func(r *pluginproto.ScanRequest) { r.ScanID = "other" },
		func(r *pluginproto.ScanRequest) { r.Generation++ },
		func(r *pluginproto.ScanRequest) { r.PlanDigest = fmt.Sprintf("%064x", 1) },
		func(r *pluginproto.ScanRequest) { r.Scope.MaxDepth = 1 },
	} {
		changed := r
		mutate(&changed)
		if _, err := w.Scan(context.Background(), changed); pluginproto.ErrorCode(err) != "RESOURCE_CHANGED" {
			t.Fatalf("changed cursor binding accepted: %v", err)
		}
	}
	if _, err := w.Scan(context.Background(), r); err != nil {
		t.Fatalf("invalid attempts destroyed a valid cursor: %v", err)
	}
}
