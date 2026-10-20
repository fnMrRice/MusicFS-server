package plugins_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func scraperHost(t *testing.T) *scraperlocal.Host {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scraper")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", path, "./plugins/scraper/local/cmd/musicfs-plugin-scraper-local")
	cmd.Dir = filepath.Join("..", "..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build scraper: %v %s", err, out)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	host, err := scraperlocal.NewHost(scraperlocal.Config{WorkerPath: path, WorkerSHA256: fmt.Sprintf("%x", sha256.Sum256(b)), WorkDir: t.TempDir(), InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func scraperScope(task string) pluginproto.Context {
	return pluginproto.Context{UserID: "owner", InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", TaskID: task, TimeoutMs: 30000}
}
func scraperTarget() enrichment.Target {
	return enrichment.Target{SourceTrackID: "source", AssetID: "asset", AssetVersion: "version", Revision: 7}
}
func scraperInput(id, kind string, body []byte) scraperlocal.Input {
	match := "exactStem"
	if kind == "audio" {
		match = "embedded"
	}
	return scraperlocal.Input{Resource: enrichment.Resource{ResourceRef: strings.Repeat(id, 48), Version: "resource-v1", Kind: kind, SizeBytes: int64(len(body)), Match: match}, Reader: bytes.NewReader(body), Validate: func() error { return nil }}
}

func TestActualScraperWorkerSearchFetchAndVersionGuards(t *testing.T) {
	host := scraperHost(t)
	var hits atomic.Int64
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(404) }))
	defer remote.Close()
	lyrics := []byte("[00:01.00]中文歌词\n[00:02.00]Translation")
	linked := append([]byte{0}, []byte("-->\x00\x03\x00"+remote.URL+"/cover")...)
	embedded := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: append([]byte{3, 'z', 'h', 'o', 0}, lyrics...)}, testkit.ScraperFrame{ID: "APIC", Body: linked})
	before := sha512.Sum512(embedded)
	inputs := []scraperlocal.Input{scraperInput("a", "audio", embedded), scraperInput("b", "lyrics", []byte("Second candidate")), scraperInput("c", "artwork", testkit.ScraperPNG())}
	var changed atomic.Bool
	inputs[0].Validate = func() error {
		if changed.Load() {
			return fmt.Errorf("RESOURCE_CHANGED: test source changed")
		}
		return nil
	}
	session, err := host.Open(context.Background(), scraperScope("search"), scraperTarget(), inputs)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	result, err := session.Search(context.Background(), []string{"lyrics", "artwork"}, []string{"zho"}, nil)
	if err != nil || len(result.Candidates) != 4 {
		t.Fatalf("actual scraper search: %+v %v", result, err)
	}
	for _, candidate := range result.Candidates {
		if candidate.Status == "unavailable" {
			continue
		}
		var output bytes.Buffer
		got, err := session.Fetch(context.Background(), candidate.CandidateID, &output)
		if err != nil || got.Target.Revision != 7 || int64(output.Len()) != candidate.SizeBytes || fmt.Sprintf("%x", sha256.Sum256(output.Bytes())) != candidate.SHA256 {
			t.Fatalf("actual scraper body validation: %v", err)
		}
	}
	if _, err = session.Fetch(context.Background(), strings.Repeat("f", 48), io.Discard); err == nil {
		t.Fatal("unsearched candidate fetched")
	}
	changed.Store(true)
	if _, err = session.Fetch(context.Background(), result.Candidates[0].CandidateID, io.Discard); err == nil {
		t.Fatal("changed source fetched stale preview")
	}
	if sha512.Sum512(embedded) != before {
		t.Fatal("scraper changed original audio")
	}
	if hits.Load() != 0 {
		t.Fatal("scraper followed an embedded picture URL")
	}
}

func TestActualScraperScopeCancellationAndSlotReclaim(t *testing.T) {
	host := scraperHost(t)
	input := scraperInput("a", "lyrics", []byte("plain lyrics"))
	scope := scraperScope("first")
	wrong := scope
	wrong.InstanceID = "another-instance"
	if _, err := host.Open(context.Background(), wrong, scraperTarget(), []scraperlocal.Input{input}); err == nil {
		t.Fatal("cross-instance scraper opened")
	}
	ctx, cancel := context.WithCancel(context.Background())
	session, err := host.Open(ctx, scope, scraperTarget(), []scraperlocal.Input{input})
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := session.Search(context.Background(), []string{"lyrics"}, nil, nil)
	if err != nil || len(firstResult.Candidates) != 1 {
		t.Fatal("first scraper search failed")
	}
	if _, err = host.Open(context.Background(), scraperScope("second"), scraperTarget(), []scraperlocal.Input{input}); err == nil {
		t.Fatal("scraper concurrency bound ignored")
	}
	start := time.Now()
	cancel()
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("scraper process cancellation exceeded cleanup limit")
	}
	next, err := host.Open(context.Background(), scraperScope("next"), scraperTarget(), []scraperlocal.Input{input})
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	result, err := next.Search(context.Background(), []string{"lyrics"}, nil, nil)
	if err != nil || len(result.Candidates) != 1 {
		t.Fatalf("reclaimed session cannot search: %v", err)
	}
	if _, err = next.Fetch(context.Background(), firstResult.Candidates[0].CandidateID, io.Discard); err == nil {
		t.Fatal("candidate crossed task sessions")
	}
}
