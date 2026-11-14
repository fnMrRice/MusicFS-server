package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/content/asmr"
	"github.com/fnMrRice/MusicFS-server/plugins/content/cdalbum"
	"github.com/fnMrRice/MusicFS-server/plugins/content/music"
	"github.com/fnMrRice/MusicFS-server/plugins/content/radio"
	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/contenttype"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/app/api"
	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/library/contenttypes"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/plugins/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type packageCatalogFixture struct {
	*packageHTTPFixture
	waves          map[string][]byte
	schemaRevision string
}

func newPackageCatalogHTTP(t *testing.T) *packageCatalogFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	wave, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	waves := map[string][]byte{}
	names := map[byte]string{}
	for i, name := range []string{"asmr-only", "cd-only", "music-only", "radio-only"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
		// 仅修改末尾 PCM 样本，用真实可播文件区分各来源的元数据。
		data := bytes.Clone(wave)
		marker := byte(i + 1)
		data[len(data)-1] = marker
		if err := os.WriteFile(filepath.Join(root, name, "track.wav"), data, 0600); err != nil {
			t.Fatal(err)
		}
		waves[name], names[marker] = data, name
	}
	probe := func(_ context.Context, source io.ReadSeeker, _ int64) (library.Observation, error) {
		if _, err := source.Seek(-1, io.SeekEnd); err != nil {
			return library.Observation{}, err
		}
		marker := make([]byte, 1)
		if _, err := io.ReadFull(source, marker); err != nil {
			return library.Observation{}, err
		}
		name, ok := names[marker[0]]
		if !ok {
			return library.Observation{}, fmt.Errorf("unknown fixture marker")
		}
		album, genre, codec, container := name+"-album", name+"-genre", "pcm_s16le", "wav"
		duration, rate := int64(6000), int64(48000)
		return library.Observation{Observation: pluginproto.Observation{MIME: "audio/wav", Codec: &codec, DurationMs: &duration},
			Container: &container, Title: &name, Artists: []string{name + "-artist"}, Album: &album, Genre: &genre, SampleRateHz: &rate}, nil
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
	declaration := worker.Manifest.Configuration
	queue := jobs.New(store)
	lib := library.New(store, queue, []library.Runtime{{ID: packageProvider, Owner: "admin", Name: "本地音乐", Worker: worker,
		ConfigRevision: "1", Configuration: declaration, ImportDefaults: declaration.ImportDefaults, RootNames: map[string]string{"music": "作品目录"}}}, probe)
	if err := queue.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(queue.Close)
	service := workpackages.New(store, store, lib, map[string]contenttype.PackageOrganizer{
		"asmr": asmr.PackageOrganizer(), "cd_album": cdalbum.PackageOrganizer(), "music": music.PackageOrganizer(), "radio": radio.PackageOrganizer(),
	})
	server := api.New(api.Options{Auth: auth.New(store, testToken, []string{origin}, time.Hour), InstanceID: "package-catalog-test", SecureCookie: true,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	server.RegisterLibrary(lib)
	server.RegisterCatalog(catalog.New(store))
	server.RegisterAlbums(albums.New(store, nil, lib))
	server.RegisterWorkPackages(service)
	registry, err := contenttypes.New(music.Descriptor(), cdalbum.Descriptor(), asmr.Descriptor(), radio.Descriptor())
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterWorks(works.New(store, registry))
	player, err := playback.New(ctx, store, lib, &audioffmpeg.Processor{}, playback.Config{CacheRoot: t.TempDir(), MaxSessions: 4, MaxCacheBytes: 16 << 20})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(player.Close)
	server.RegisterPlayback(player)
	cookie, session := login(t, server)
	return &packageCatalogFixture{packageHTTPFixture: &packageHTTPFixture{server: server, store: store, service: service, cookie: cookie, csrf: session.CSRFToken, root: root}, waves: waves, schemaRevision: declaration.SchemaRevision}
}

func (f *packageCatalogFixture) call(t *testing.T, method, path string, body any, key string, status int, schema string, result any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		path = "/api/v1" + path
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(raw))
	r.AddCookie(f.cookie)
	r.Header.Set("Origin", origin)
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", key)
	w := httptest.NewRecorder()
	f.server.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s returned %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	if schema != "" {
		testkit.ValidateHTTP(t, schema, w.Body.Bytes())
	}
	if result != nil {
		if err := json.Unmarshal(w.Body.Bytes(), result); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

func (f *packageCatalogFixture) scan(t *testing.T, key string) {
	t.Helper()
	var queued struct {
		JobID string `json:"jobId"`
	}
	f.call(t, "POST", "/provider-instances/"+packageProvider+"/scan-jobs", library.ScanInput{Mode: "full", SchemaRevision: f.schemaRevision, RootIDs: []string{"music"}}, key, 202, "", &queued)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var job jobs.Job
		f.call(t, "GET", "/scan-jobs/"+queued.JobID, nil, "", 200, "ScanJob", &job)
		if job.Status == "succeeded" {
			if job.Progress.Observed != len(f.waves) {
				t.Fatal("scan did not observe every fixture", job.Progress.Observed)
			}
			return
		}
		if jobs.IsTerminal(job.Status) {
			t.Fatal("scan failed", job.Status, job.Error)
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("scan timed out")
}

func (f *packageCatalogFixture) importCopy(t *testing.T, name, contentType, kind string) workpackages.Package {
	t.Helper()
	var work works.Work
	f.call(t, "POST", "/works", works.CreateRequest{ContentType: contentType, Title: name, ExternalIDs: []works.ExternalID{}, Credits: []works.Credit{}, Tags: []string{}}, "create-"+name, 201, "Work", &work)
	var preview workpackages.Preview
	f.call(t, "POST", "/works/"+work.ID+"/package-previews", workpackages.Selection{ProviderID: packageProvider, RootID: "music", Path: name, Kind: kind}, "", 200, "WorkPackagePreview", &preview)
	var imported workpackages.Package
	f.call(t, "POST", "/works/"+work.ID+"/packages", map[string]string{"previewId": preview.ID}, "import-"+name, 200, "WorkPackage", &imported)
	return imported
}

func (f *packageCatalogFixture) otherUserCookie(t *testing.T) *http.Cookie {
	t.Helper()
	// 当前登录只提供管理员账号；补入第二个账号夹具，后续仍经真实 HTTP 鉴权检查。
	if _, err := f.store.DB.ExecContext(t.Context(), "INSERT INTO users(id,role) VALUES(?,?)", "other", "admin"); err != nil {
		t.Fatal(err)
	}
	value := strings.Repeat("a", 48)
	err := f.store.SaveAuth(t.Context(), auth.Session{ID: auth.Digest(value), User: auth.User{ID: "other", Role: "admin"}, CSRFToken: "other-user-csrf", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: api.CookieName, Value: value}
}
