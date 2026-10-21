package inprocess

import (
	"net/http"
	"sync"
)

// Handlers 在任务数据通道关闭后等待已进入的读写退出，再释放宿主资源。
type Handlers struct {
	mu     sync.Mutex
	closed bool
	active sync.WaitGroup
}

func (h *Handlers) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			w.WriteHeader(http.StatusGone)
			return
		}
		h.active.Add(1)
		h.mu.Unlock()
		defer h.active.Done()
		next.ServeHTTP(w, r)
	})
}

// Wait 在 HTTP 服务已关闭后调用；禁止新请求进入并等待已有操作结束。
func (h *Handlers) Wait() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.active.Wait()
}
