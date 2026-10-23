package library_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScrapeSourceActualNCMDecoder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ncm-worker")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-mod=readonly", "-o", path, "./plugins/decoder/ncm/cmd/musicfs-plugin-ncm")
	cmd.Dir = testkit.RepositoryRoot()
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build decoder: %v %s", err, out)
	}
	worker, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{WorkerPath: path, WorkerSHA256: fmt.Sprintf("%x", sha256.Sum256(worker)), WorkDir: t.TempDir(), MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	tag := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00Embedded NCM lyrics")})
	payload = append(tag, payload...)
	container, err := testkit.BuildNCM(payload, map[string]any{"format": "flac"})
	if err != nil {
		t.Fatal(err)
	}
	service, _, _ := scrapeFixture(t, "track.ncm", container)
	input, err := service.OpenScrape(context.Background(), "owner", "task", "track", []string{"lyrics"})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err = input.DecodeAudio(context.Background(), decoder); err != nil {
		t.Fatal(err)
	}
	if len(input.Issues) != 0 || len(input.Inputs) != 2 || input.Inputs[0].Resource.SizeBytes != int64(len(payload)) {
		t.Fatal("decoded descriptor mismatch")
	}
	actual, err := io.ReadAll(io.NewSectionReader(input.Inputs[0].Reader, 0, input.Inputs[0].Resource.SizeBytes))
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("NCM decoded bytes differ", err)
	}
	if len(scrapeSearch(t, input).Candidates) != 2 {
		t.Fatal("NCM embedded and sidecar lyrics missing")
	}
	if err = input.Publish(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
