package plugins_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func buildNCMWorker(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ncm-worker")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", path, "./plugins/decoder/ncm/cmd/musicfs-plugin-ncm")
	cmd.Dir = filepath.Join("..", "..", "..")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, fmt.Sprintf("%x", sha256.Sum256(b))
}

type countedNCMInput struct {
	*bytes.Reader
	bytes atomic.Int64
	max   atomic.Int64
}

func (r *countedNCMInput) ReadAt(dst []byte, offset int64) (int, error) {
	r.bytes.Add(int64(len(dst)))
	for old := r.max.Load(); int64(len(dst)) > old; old = r.max.Load() {
		if r.max.CompareAndSwap(old, int64(len(dst))) {
			break
		}
	}
	return r.Reader.ReadAt(dst, offset)
}

func TestActualNCMWorker(t *testing.T) {
	worker, hash := buildNCMWorker(t)
	cfg := ncmdecoder.Config{WorkerPath: worker, WorkerSHA256: hash, WorkDir: t.TempDir(), MaxConcurrent: 1}
	p, err := ncmdecoder.NewProcessor(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []string{"vbr-untagged.mp3", "lossless.flac"} {
		t.Run(fixture, func(t *testing.T) {
			plain, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", fixture))
			if err != nil {
				t.Fatal(err)
			}
			// Repetition makes cross-chunk reads observable without a large fixture file.
			plain = bytes.Repeat(plain, 16)
			encrypted, err := testkit.BuildNCMWithOptions(plain, map[string]any{"musicName": "Synthetic track", "album": "CC0", "artist": [][]any{{"MusicFS", 1}}, "duration": 6000}, testkit.NCMOptions{Cover: []byte("ignored cover"), CoverPadding: 3 << 20})
			if err != nil {
				t.Fatal(err)
			}
			input := &countedNCMInput{Reader: bytes.NewReader(encrypted)}
			decoded, err := p.Open(context.Background(), "random", "asset", input, int64(len(encrypted)))
			if err != nil {
				t.Fatal(err)
			}
			defer decoded.Close()
			if decoded.Size() != int64(len(plain)) || input.bytes.Load() > 4096 {
				t.Fatalf("unexpected size/startup read: %d / %d", decoded.Size(), input.bytes.Load())
			}
			metadata := decoded.Metadata()
			if metadata.Title != "Synthetic track" || metadata.Album != "CC0" || len(metadata.Artists) != 1 || metadata.DurationMs == nil || *metadata.DurationMs != 6000 {
				t.Fatalf("unexpected metadata: %+v", metadata)
			}
			metadata.Artists[0] = "changed"
			*metadata.DurationMs = 1
			if decoded.Metadata().Artists[0] != "MusicFS" || *decoded.Metadata().DurationMs != 6000 {
				t.Fatal("metadata aliases mutable state")
			}
			var wg sync.WaitGroup
			for seed := uint64(1); seed <= 8; seed++ {
				wg.Go(func() {
					random := rand.New(rand.NewPCG(seed, 7))
					for range 15 {
						offset := random.IntN(len(plain) + 1)
						buffer := make([]byte, 1+random.IntN(150000))
						n, readErr := decoded.ReadAt(buffer, int64(offset))
						want := min(len(buffer), len(plain)-offset)
						if n != want || !bytes.Equal(buffer[:n], plain[offset:offset+want]) || (want < len(buffer) && readErr != io.EOF) || (want == len(buffer) && readErr != nil) {
							t.Errorf("random read mismatch offset=%d size=%d n=%d err=%v", offset, len(buffer), n, readErr)
							return
						}
					}
				})
			}
			wg.Wait()
			if input.max.Load() > 65536 {
				t.Fatalf("unbounded source read: %d", input.max.Load())
			}
			if _, err := p.Open(context.Background(), "busy", "asset", input, int64(len(encrypted))); err == nil || !strings.HasPrefix(err.Error(), "RATE_LIMITED") {
				t.Fatalf("concurrency limit ignored: %v", err)
			}
			if err := decoded.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := decoded.ReadAt(make([]byte, 4), 0); err == nil {
				t.Fatal("closed decoded input remains readable")
			}
		})
	}
	t.Run("corrupt-and-cancel", func(t *testing.T) {
		bad := bytes.Repeat([]byte{0xff}, 512)
		if _, err := p.Open(context.Background(), "bad", "asset", bytes.NewReader(bad), int64(len(bad))); err == nil || !strings.Contains(err.Error(), "UNSUPPORTED") {
			t.Fatalf("corrupt input accepted: %v", err)
		}
		plain, err := os.ReadFile(filepath.Join("..", "..", "testdata", "audio", "lossless.flac"))
		if err != nil {
			t.Fatal(err)
		}
		encrypted, err := testkit.BuildNCM(plain, nil)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		decoded, err := p.Open(ctx, "cancel", "asset", bytes.NewReader(encrypted), int64(len(encrypted)))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		start := time.Now()
		cancel()
		if err := decoded.Close(); err != nil || time.Since(start) > 3*time.Second {
			t.Fatalf("cancel did not reclaim worker promptly: %v (%s)", err, time.Since(start))
		}
		if _, err := decoded.ReadAt(make([]byte, 4), 0); err == nil {
			t.Fatal("canceled decoded input remains readable")
		}
		again, err := p.Open(context.Background(), "again", "asset", bytes.NewReader(encrypted), int64(len(encrypted)))
		if err != nil {
			t.Fatalf("cancellation did not free worker slot: %v", err)
		}
		if err := again.Close(); err != nil {
			t.Fatalf("reopened worker cleanup failed: %v", err)
		}
	})
	t.Run("hash-rejection", func(t *testing.T) {
		wrong := cfg
		wrong.WorkerSHA256 = strings.Repeat("0", 64)
		if _, err := ncmdecoder.NewProcessor(wrong); err == nil || !strings.HasPrefix(err.Error(), "ACCESS_DENIED") {
			t.Fatalf("wrong checksum accepted: %v", err)
		}
		replaceNCMWorkerWithChangedCopy(t, worker)
		if _, err := p.Open(context.Background(), "tampered", "asset", bytes.NewReader([]byte("bad")), 3); err == nil || !strings.HasPrefix(err.Error(), "ACCESS_DENIED") {
			t.Fatalf("changed executable accepted: %v", err)
		}
	})
}

func replaceNCMWorkerWithChangedCopy(t *testing.T, worker string) {
	t.Helper()
	data, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(worker)
	if err != nil {
		t.Fatal(err)
	}
	// Replacing the path avoids Linux ETXTBSY when an executed inode is not yet writable.
	file, err := os.CreateTemp(filepath.Dir(worker), "ncm-worker-changed-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	defer os.Remove(file.Name())
	if _, err := file.Write(append(data, []byte("changed")...)); err != nil {
		t.Fatal(err)
	}
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file.Name(), worker); err != nil {
		t.Fatal(err)
	}
}
