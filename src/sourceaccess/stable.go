package sourceaccess

import (
	"context"
	"fmt"
	"os"
	"sync"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type stableProtection struct {
	file    *os.File
	check   func() error
	release func() error
	notice  <-chan os.Signal
}

// StableReader holds OS-enforced read protection, not an advisory application
// lock. Callers must retain it through publication and close it afterwards.
type StableReader struct {
	mu         sync.Mutex
	protection stableProtection
	entry      pluginproto.Entry
	root, path string
	checkPath  func() error
	closed     bool
	failure    error
	done       chan struct{}
	stop       func() bool
}

// Stable refuses filesystems or platforms that cannot guarantee a stable read.
// A protected handle is independent of the ordinary source handle's lifetime.
func (h *Handle) Stable(ctx context.Context) (*StableReader, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if h.checkPath == nil {
		return nil, fmt.Errorf("UNSUPPORTED: stable source binding is unavailable")
	}
	h.metadataMu.Lock()
	p, err := protectStableFile(h.File)
	h.metadataMu.Unlock()
	if err != nil {
		return nil, err
	}
	s := &StableReader{protection: p, entry: h.Entry, root: h.rootID, path: h.path, checkPath: h.checkPath, done: make(chan struct{})}
	if err := s.Validate(); err != nil {
		s.Close()
		return nil, err
	}
	s.stop = context.AfterFunc(ctx, func() { s.fail(ctx.Err()) })
	if p.notice != nil {
		go func() {
			for {
				select {
				case <-s.done:
					return
				case <-p.notice:
					s.mu.Lock()
					if !s.closed {
						if err := p.check(); err != nil {
							s.closeLocked(err)
						}
					}
					s.mu.Unlock()
				}
			}
		}()
	}
	return s, nil
}

func (s *StableReader) activeLocked() error {
	if s.failure != nil {
		return s.failure
	}
	if s.closed {
		return os.ErrClosed
	}
	return s.protection.check()
}

func (s *StableReader) ReadAt(p []byte, offset int64) (int, error) {
	s.mu.Lock()
	err := s.activeLocked()
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}
	n, readErr := s.protection.file.ReadAt(p, offset)
	s.mu.Lock()
	err = s.activeLocked()
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}
	return n, readErr
}

// Validate checks protection, the open file version, and the original authorized
// root/path binding. A rename cannot quietly substitute another file for it.
func (s *StableReader) Validate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.validateLocked()
}

// Publish retains protection while a short publication transaction runs. A
// pending lease-break or cancellation cannot release the file in that interval.
func (s *StableReader) Publish(commit func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateLocked(); err != nil {
		return err
	}
	return commit()
}

func (s *StableReader) validateLocked() error {
	if err := s.activeLocked(); err != nil {
		return err
	}
	entry, err := describe(s.protection.file, s.root, s.path)
	if err != nil || entry.ObservedVersion != s.entry.ObservedVersion {
		return fmt.Errorf("RESOURCE_CHANGED: protected source version changed")
	}
	if err := s.checkPath(); err != nil {
		return err
	}
	return s.activeLocked()
}

func (s *StableReader) closeLocked(failure error) error {
	if s.closed {
		return nil
	}
	s.closed, s.failure = true, failure
	err := s.protection.release()
	if closeErr := s.protection.file.Close(); err == nil {
		err = closeErr
	}
	close(s.done)
	return err
}

func (s *StableReader) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeLocked(err)
}

func (s *StableReader) Close() error {
	if s.stop != nil {
		s.stop()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closeLocked(nil)
}
