package builtinruntime_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type heldInput struct {
	*bytes.Reader
	hold             atomic.Bool
	once             sync.Once
	entered, release chan struct{}
}

func (r *heldInput) ReadAt(body []byte, offset int64) (int, error) {
	if r.hold.Load() {
		r.once.Do(func() { close(r.entered) })
		<-r.release
	}
	return r.Reader.ReadAt(body, offset)
}

func TestBuiltinCloseDrainsHostReadsBeforeReleasingLease(t *testing.T) {
	plain, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests", "testdata", "audio", "lossless.flac"))
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := testkit.BuildNCM(plain, nil)
	if err != nil {
		t.Fatal(err)
	}
	input := &heldInput{Reader: bytes.NewReader(encrypted), entered: make(chan struct{}), release: make(chan struct{})}
	processor, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true, MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	decoded, err := processor.Open(ctx, "drain", "asset", input, int64(len(encrypted)))
	if err != nil {
		t.Fatal(err)
	}
	input.hold.Store(true)
	readDone := make(chan struct{})
	go func() { _, _ = decoded.ReadAt(make([]byte, 8), 0); close(readDone) }()
	select {
	case <-input.entered:
	case <-time.After(time.Second):
		close(input.release)
		t.Fatal("builtin decoder did not reach the controlled host read")
	}
	cancel()
	closed := make(chan struct{})
	go func() { decoded.Close(); close(closed) }()
	select {
	case <-closed:
		close(input.release)
		t.Fatal("builtin close released host resources during an active read")
	case <-time.After(30 * time.Millisecond):
	}
	close(input.release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("builtin close did not finish after the host read completed")
	}
	<-readDone
}
