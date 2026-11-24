package playback

import (
	"context"
	"io"
	"io/fs"
	"sync"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
)

// 首次 GET 才打开一次平台音频，所有 Range 都从该会话固定的字节表示读取。
type onlineRepresentation struct {
	size, duration int64
	once           sync.Once
	ready          chan struct{}
	body           onlinecache.SeekableAudio
	err            error
}

func (r *onlineRepresentation) open(ctx context.Context, s *Service, a *session) (io.ReadSeeker, error) {
	if err := a.network.checkAccess(ctx); err != nil {
		return nil, err
	}
	r.once.Do(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if err := a.attemptCtx.Err(); err != nil {
			r.err = err
			close(r.ready)
			return
		}
		s.downloadTasks.Add(1)
		go func() { defer s.downloadTasks.Done(); r.start(s, a) }()
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-r.ready:
	}
	if r.err != nil {
		return nil, r.err
	}
	if err := a.network.checkAccess(ctx); err != nil {
		return nil, err
	}
	return &onlineCursor{ctx: ctx, input: a.network, shared: r}, nil
}

func (r *onlineRepresentation) start(s *Service, a *session) {
	ctx := a.attemptCtx
	if a.network.preferences.LimitBytes == 0 {
		if !s.budget.TryAcquire(r.size) {
			r.err = domain.Fail("RATE_LIMITED", "Temporary playback storage is full")
			close(r.ready)
			return
		}
		defer s.budget.Release(r.size)
	}
	body, err := s.networkReader(ctx, a)
	if err == nil {
		var ok bool
		if r.body, ok = body.(onlinecache.SeekableAudio); !ok {
			body.Close()
			body = nil
			err = domain.Fail("UNSUPPORTED", "Online audio representation does not support byte ranges")
		}
	}
	r.err = err
	close(r.ready)
	if body != nil {
		<-ctx.Done()
		body.Close()
	}
}

type onlineCursor struct {
	ctx    context.Context
	input  networkInput
	shared *onlineRepresentation
	offset int64
}

func (c *onlineCursor) Read(p []byte) (int, error) {
	if err := c.input.checkAccess(c.ctx); err != nil {
		return 0, err
	}
	if c.offset >= c.shared.size {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), c.shared.size-c.offset)]
	n, err := c.shared.body.ReadAvailableAt(c.ctx, p, c.offset, c.shared.size)
	if revoked := c.input.checkAccess(c.ctx); revoked != nil {
		return 0, revoked
	}
	c.offset += int64(n)
	return n, err
}

func (c *onlineCursor) Seek(off int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		off += c.offset
	case io.SeekEnd:
		off += c.shared.size
	default:
		return 0, fs.ErrInvalid
	}
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	c.offset = off
	return off, nil
}
