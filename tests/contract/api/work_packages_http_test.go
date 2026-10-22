package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type packageHTTPFixture struct {
	server             *api.Server
	store              *sqlite.Store
	service            *workpackages.Service
	cookie             *http.Cookie
	csrf, root, workID string
}

const packageProvider = "316664dc-7106-5acd-8df7-88c9d0d9baef"

func newPackageHTTP(t *testing.T) *packageHTTPFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	for _, dir := range []string{"RJ123456/正篇/无BGM", "RJ123456/特典/空目录", "RJ123456/资料"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	wave, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"RJ123456/正篇/无BGM/02.wav", "RJ123456/正篇/无BGM/10.wav"} {
		if err = os.WriteFile(filepath.Join(root, name), wave, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "RJ123456/资料/说明.txt"), []byte("中文脚本 <script>alert(1)</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	var picture bytes.Buffer
	if err = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "RJ123456/资料/封面.png"), picture.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	worker, err := pluginhost.Start(ctx, pluginhost.Config{Builtin: true, UserID: "admin", InstanceID: packageProvider, StateDir: t.TempDir(), Roots: []sourceaccess.RootConfig{{ID: "music", Path: root}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { worker.Close() })
	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "library.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	lib := library.New(store, jobs.New(store), []library.Runtime{{ID: packageProvider, Owner: "admin", Name: "本地音声", Worker: worker, ConfigRevision: "1", ImportDefaults: pluginproto.ImportOptions{Recursive: true}, RootNames: map[string]string{"music": "作品目录"}}}, nil)
	service := workpackages.New(store, store, lib, map[string]contenttype.PackageOrganizer{"asmr": asmr.PackageOrganizer(), "music": music.PackageOrganizer()})
	server, _ := testServer()
	server.RegisterLibrary(lib)
	server.RegisterWorkPackages(service)
	registry, err := contenttypes.New(music.Descriptor(), asmr.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterWorks(works.New(store, registry))
	cookie, session := login(t, server)
	work, err := store.CreateWork(ctx, "admin", works.CreateRequest{ContentType: "asmr", Title: "雨夜音声"}, "package-work-create")
	if err != nil {
		t.Fatal(err)
	}
	return &packageHTTPFixture{server, store, service, cookie, session.CSRFToken, root, work.ID}
}

func (f *packageHTTPFixture) request(t *testing.T, method, suffix string, body any, key string, change func(*http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "/api/v1/works/"+f.workID+suffix, bytes.NewReader(raw))
	r.AddCookie(f.cookie)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	if change != nil {
		change(r)
	}
	w := httptest.NewRecorder()
	f.server.ServeHTTP(w, r)
	if w.Code >= 400 {
		testkit.ValidateHTTP(t, "Error", w.Body.Bytes())
	}
	return w
}

func (f *packageHTTPFixture) preview(t *testing.T) workpackages.Preview {
	return f.previewKind(t, "directory")
}

func (f *packageHTTPFixture) previewKind(t *testing.T, kind string) workpackages.Preview {
	t.Helper()
	w := f.request(t, "POST", "/package-previews", workpackages.Selection{ProviderID: packageProvider, RootID: "music", Path: "RJ123456", Kind: kind}, "", nil)
	if w.Code != 200 {
		t.Fatal("preview", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "WorkPackagePreview", w.Body.Bytes())
	var preview workpackages.Preview
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	return preview
}

func TestWorkPackagesHTTPImportsNestedAudioAndAttachments(t *testing.T) {
	f := newPackageHTTP(t)
	for suffix, schema := range map[string]string{"/package-sources": "WorkPackageSources", "/package-directories?providerInstanceId=" + packageProvider + "&rootId=music&path=.": "WorkPackageDirectories", "/packages": "WorkPackages"} {
		w := f.request(t, "GET", suffix, nil, "", nil)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
	}
	preview := f.preview(t)
	if preview.Counts.Audio != 2 || preview.Counts.Attachments != 2 || preview.Counts.Directories != 6 {
		t.Fatal("tree counts", preview.Counts)
	}
	before, err := f.store.ListTracks(context.Background(), "admin", "", "", 20)
	if err != nil || len(before.Items) != 0 {
		t.Fatal("preview published tracks", err)
	}
	body := map[string]string{"previewId": preview.ID}
	w := f.request(t, "POST", "/packages", body, "package-confirm-key", nil)
	if w.Code != 200 {
		t.Fatal("import", w.Code, w.Body.String())
	}
	testkit.ValidateHTTP(t, "WorkPackage", w.Body.Bytes())
	var imported workpackages.Package
	if err = json.Unmarshal(w.Body.Bytes(), &imported); err != nil {
		t.Fatal(err)
	}
	if imported.ID == "" || imported.Revision != 1 {
		t.Fatal("missing package identity")
	}
	for _, private := range []string{"sourcePath", "rootIdentity", "observedVersion", f.root} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatal("private source data leaked", private)
		}
	}
	for _, node := range imported.Nodes {
		if node.Kind == "audio" && (node.Track == nil || node.Track.ID != node.SourceTrackID || node.AssetID == "") {
			t.Fatal("audio not linked", node)
		}
		if node.Kind != "attachment" {
			continue
		}
		attachment := "/packages/" + imported.ID + "/attachments/" + node.ID
		response := f.request(t, "GET", attachment, nil, "", nil)
		if response.Code != 200 {
			t.Fatal("attachment", response.Code, response.Body.String())
		}
		if node.AttachmentKind == "text" && (!strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain") || !strings.Contains(response.Body.String(), "<script>")) {
			t.Fatal("text preview changed or executes content")
		}
		response = f.request(t, "GET", attachment+"?download=1", nil, "", nil)
		if response.Code != 200 || !strings.HasPrefix(response.Header().Get("Content-Disposition"), "attachment;") {
			t.Fatal("attachment download", response.Code)
		}
	}
	// 成功确认会移除内存预览，后续确认仍从数据库重放。
	if replay, err := f.service.Import(context.Background(), "admin", f.workID, preview.ID, "package-confirm-key"); err != nil || replay.ID != imported.ID || replay.Revision != 1 {
		t.Fatal("durable replay", err)
	}
	next := f.preview(t)
	w = f.request(t, "POST", "/packages", map[string]string{"previewId": next.ID}, "package-refresh-key", nil)
	if w.Code != 200 {
		t.Fatal("refresh", w.Code, w.Body.String())
	}
	var refreshed workpackages.Package
	json.Unmarshal(w.Body.Bytes(), &refreshed)
	if refreshed.ID != imported.ID || refreshed.Revision != 2 {
		t.Fatal("refresh created duplicate")
	}
	for i, node := range refreshed.Nodes {
		if node.ID != imported.Nodes[i].ID || node.SourceTrackID != imported.Nodes[i].SourceTrackID {
			t.Fatal("refresh changed identities")
		}
	}
}

func TestWorkPackagesHTTPRejectsChangedPreviewAndUnauthorizedAccess(t *testing.T) {
	f := newPackageHTTP(t)
	preview := f.preview(t)
	if err := os.WriteFile(filepath.Join(f.root, "RJ123456/资料/说明.txt"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	w := f.request(t, "POST", "/packages", map[string]string{"previewId": preview.ID}, "changed-package-key", nil)
	if w.Code != 409 {
		t.Fatal("changed preview accepted", w.Code, w.Body.String())
	}
	items, err := f.store.WorkPackages(context.Background(), "admin", f.workID)
	if err != nil || len(items) != 0 {
		t.Fatal("failed import published", err)
	}
	for _, suffix := range []string{"/packages", "/package-sources"} {
		w = f.request(t, "GET", suffix, nil, "", func(r *http.Request) { r.Header.Del("Cookie") })
		if w.Code != 401 {
			t.Fatal("anonymous access", w.Code)
		}
	}
	w = f.request(t, "POST", "/package-previews", workpackages.Selection{ProviderID: packageProvider, RootID: "music", Path: "../outside", Kind: "directory"}, "", nil)
	if w.Code != 400 {
		t.Fatal("traversal accepted", w.Code)
	}
	w = f.request(t, "POST", "/packages", map[string]string{"previewId": preview.ID}, "no-csrf-package", func(r *http.Request) { r.Header.Del("X-CSRF-Token") })
	if w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	other, err := f.store.CreateWork(context.Background(), "other", works.CreateRequest{ContentType: "asmr", Title: "private"}, "private-package-work")
	if err != nil {
		t.Fatal(err)
	}
	f.workID = other.ID
	w = f.request(t, "GET", "/packages", nil, "", nil)
	if w.Code != 404 {
		t.Fatal("other owner exposed", w.Code)
	}
}
