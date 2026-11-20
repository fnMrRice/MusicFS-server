package recommendations

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

// 同一作用域的一轮读取和历史入栈共享完成信号。最后一个等待者离开后取消该轮。
type pageFlight struct {
	done               chan struct{}
	cancel             context.CancelFunc
	day                string
	session            string
	waiters            int
	abandoned, fetched bool
	result             Response
	err                error
}

func (s *Service) currentPage(ctx context.Context, owner string, scope pageScope, day string, runtimes []Runtime, request Request) (Response, error) {
	for {
		if err := ctx.Err(); err != nil {
			return Response{}, err
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			return Response{}, domain.Fail("SOURCE_OFFLINE", "Recommendation service is draining")
		}
		f := s.pages[scope.key]
		if f == nil {
			if len(s.pages) >= s.config.MaxEntries {
				s.mu.Unlock()
				return Response{}, domain.Fail("RATE_LIMITED", "Recommendation pages are busy")
			}
			run, cancel := context.WithCancel(s.ctx)
			f = &pageFlight{done: make(chan struct{}), cancel: cancel, day: day, session: scope.session}
			s.pages[scope.key] = f
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer cancel()
				f.result, f.fetched, f.err = s.readOrRefresh(run, owner, scope, day, runtimes, request)
				s.mu.Lock()
				delete(s.pages, scope.key)
				close(f.done)
				s.mu.Unlock()
			}()
		}
		// 跨过服务器午夜时，后一日的首开仍要在前一轮结束后读取自己的当日结果。
		joined := !f.abandoned && f.day == day && f.session == scope.session
		if joined {
			f.waiters++
		}
		s.mu.Unlock()
		select {
		case <-f.done:
			if !joined || (request.Refresh && !f.fetched && f.err == nil) {
				continue
			}
			if f.err != nil {
				return Response{}, f.err
			}
			result := Response{Sources: make([]Source, len(f.result.Sources)), HistoryDepth: f.result.HistoryDepth}
			for i, src := range f.result.Sources {
				result.Sources[i] = cloneSource(src)
			}
			return result, nil
		case <-ctx.Done():
			if joined {
				s.mu.Lock()
				f.waiters--
				if f.waiters == 0 {
					f.abandoned = true
					f.cancel()
				}
				s.mu.Unlock()
			}
			return Response{}, ctx.Err()
		case <-s.ctx.Done():
			return Response{}, domain.Fail("SOURCE_OFFLINE", "Recommendation service is draining")
		}
	}
}
