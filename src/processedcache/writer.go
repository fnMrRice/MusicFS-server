package processedcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash"
	"os"
	"path/filepath"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type Writer struct {
	mu       sync.Mutex
	manager  *Manager
	identity Identity
	key, id  string
	path     string
	file     *os.File
	hash     hash.Hash
	size     int64
	finished bool
	failure  error
}

// Begin admits one cache writer per key and at most eight in-flight outputs.
// A nil writer means caching is unavailable; processing can still proceed.
func (m *Manager) Begin(identity Identity) *Writer {
	key, err := identity.Key()
	if err != nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil || m.entries[key] != nil || m.writers[key] != nil || len(m.writers) >= 8 || len(m.entries)+len(m.writers)+len(m.garbage) >= MaxEntries {
		return nil
	}
	id := domain.NewID()
	path := filepath.Join(m.config.Root, id+".part")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil
	}
	w := &Writer{manager: m, identity: identity, key: key, id: id, path: path, file: f, hash: sha256.New()}
	m.writers[key] = w
	m.wg.Add(1)
	return w
}

// Write deliberately reports caching failures only to Complete. It never turns
// an otherwise deliverable processing stream into a failed playback request.
func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return len(p), nil
	}
	if w.manager.ctx.Err() != nil || !w.manager.budget.TryAcquire(int64(len(p))) {
		w.failure = errors.New("processed cache budget or lifecycle limit reached")
		w.abortLocked()
		return len(p), nil
	}
	n, err := w.file.Write(p)
	w.manager.budget.Release(int64(len(p) - n))
	w.hash.Write(p[:n])
	w.size += int64(n)
	if err != nil || n != len(p) {
		w.failure = errors.New("processed cache write failed")
		w.abortLocked()
	}
	return len(p), nil
}

// Complete runs publish without any Manager mutex. The caller's callback must
// atomically recheck current auth, asset and playback attempt in its repository.
func (w *Writer) Complete(ctx context.Context, publish func(context.Context, Record) error) (*Pin, error) {
	ctx, cancel := context.WithCancel(ctx)
	unlink := context.AfterFunc(w.manager.ctx, cancel)
	defer func() { unlink(); cancel() }()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		if w.failure != nil {
			return nil, w.failure
		}
		return nil, errors.New("processed cache writer already finished")
	}
	ok := false
	defer func() {
		if !ok {
			w.abortLocked()
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if w.manager.ctx.Err() != nil || publish == nil || w.size == 0 {
		return nil, errors.New("processed cache cannot be published")
	}
	if err := w.file.Sync(); err != nil {
		return nil, err
	}
	if err := w.file.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(w.path, w.manager.path(w.id)); err != nil {
		return nil, err
	}
	w.path = w.manager.path(w.id)
	info, err := os.Lstat(w.path)
	if err != nil || !info.Mode().IsRegular() || info.Size() != w.size {
		return nil, errors.New("processed cache output changed")
	}
	now := w.manager.now()
	r := Record{ID: w.id, Key: w.key, Identity: w.identity, Size: w.size, SHA256: hex.EncodeToString(w.hash.Sum(nil)), CreatedAt: now, LastUsedAt: now}
	if err = publish(ctx, r); err != nil {
		return nil, err
	}
	w.manager.mu.Lock()
	e := &entry{record: r, info: info, refs: 1, lastRelease: now}
	w.manager.entries[w.key] = e
	w.manager.mu.Unlock()
	w.finishLocked()
	ok = true
	return &Pin{manager: w.manager, entry: e}, nil
}

func (w *Writer) Abort() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.finished {
		w.abortLocked()
	}
}
func (w *Writer) abortLocked() {
	w.file.Close()
	if err := os.Remove(w.path); err == nil || os.IsNotExist(err) {
		w.manager.budget.Release(w.size)
		// A DB commit may have succeeded before its caller observed an error.
		// Remove any such index asynchronously even when the file is gone.
		w.manager.mu.Lock()
		w.manager.garbage[w.path] = garbage{id: w.id, path: w.path, removed: true}
		w.manager.mu.Unlock()
	} else {
		w.manager.mu.Lock()
		w.manager.garbage[w.path] = garbage{id: w.id, path: w.path, size: w.size}
		w.manager.mu.Unlock()
	}
	w.finishLocked()
}
func (w *Writer) finishLocked() {
	w.finished = true
	w.manager.mu.Lock()
	delete(w.manager.writers, w.key)
	w.manager.mu.Unlock()
	w.manager.wg.Done()
}
