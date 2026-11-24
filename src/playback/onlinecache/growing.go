package onlinecache

import (
	"context"
	"io"
	"os"
	"sync"
)

// growingReader 顺序读取临时文件中已收到的字节。浏览器暂停时，下载仍可以写满文件并发布版本。
// 读临时文件、关闭文件及切换到已发布副本互斥，Windows 上也无需在重命名时打开第二个句柄。
type growingReader struct {
	mu        sync.Mutex
	ready     *sync.Cond
	file      *os.File
	available int64
	offset    int64
	result    *readHandle
	finished  bool
	closed    bool
	err       error
}

func newGrowingReader() *growingReader {
	r := &growingReader{}
	r.ready = sync.NewCond(&r.mu)
	return r
}

func (r *growingReader) use(f *os.File) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.file = f
}

// 作为 MultiWriter 的最后一项，此时 p 已完整写入磁盘。
func (r *growingReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return 0, io.ErrClosedPipe
	}
	r.available += int64(len(p))
	r.ready.Broadcast()
	return len(p), nil
}

func (r *growingReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n, err := r.readAvailable(context.Background(), p, r.offset, 0)
	r.offset += int64(n)
	return n, err
}

// 等待指定偏移时只取消当前读者。末字节必须等下载完成长度校验再交付。
func (r *growingReader) readAvailable(ctx context.Context, p []byte, offset, size int64) (int, error) {
	var stop func() bool
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		available := r.available
		if size > 0 && !r.finished {
			available = min(available, size-1)
		}
		switch {
		case r.closed:
			return 0, io.ErrClosedPipe
		case r.err != nil:
			return 0, r.err
		case len(p) == 0:
			return 0, nil
		case r.file != nil && offset < available:
			return r.file.ReadAt(p[:min(int64(len(p)), available-offset)], offset)
		case r.finished:
			return 0, io.EOF
		default:
			if stop == nil && ctx.Done() != nil {
				stop = context.AfterFunc(ctx, func() { r.mu.Lock(); r.ready.Broadcast(); r.mu.Unlock() })
			}
			r.ready.Wait()
		}
	}
}

func (r *growingReader) abort(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished {
		r.err = err
		r.ready.Broadcast()
	}
}

func (r *growingReader) complete(h *readHandle, err error) {
	r.mu.Lock()
	if r.err == nil {
		r.err = err
	}
	r.finished = true
	if h != nil && !r.closed {
		r.result, r.file = h, h.file
		h = nil
	}
	r.ready.Broadcast()
	r.mu.Unlock()
	if h != nil {
		h.Close()
	}
}

func (r *growingReader) Close() error {
	r.mu.Lock()
	r.closed = true
	h := r.result
	r.result, r.file = nil, nil
	r.ready.Broadcast()
	r.mu.Unlock()
	if h != nil {
		return h.Close()
	}
	return nil
}
