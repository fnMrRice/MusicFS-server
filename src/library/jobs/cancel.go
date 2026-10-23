package jobs

import "context"

type execution struct {
	owner  string
	cancel context.CancelFunc
	done   chan struct{}
}

// InterruptJob 只负责回收执行资源，持久取消与发布保护由存储事务完成。
func (s *Service) InterruptJob(owner, id string) <-chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if run, ok := s.running[id]; ok && run.owner == owner {
		run.cancel()
		return run.done
	}
	done := make(chan struct{})
	close(done)
	return done
}

func (s *Service) execute(parent context.Context, job Job, handler Handler) (int, error) {
	ctx, cancel := context.WithCancel(parent)
	run := execution{owner: job.Owner, cancel: cancel, done: make(chan struct{})}
	s.mu.Lock()
	s.running[job.ID] = run
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.running, job.ID)
		close(run.done)
		s.mu.Unlock()
	}()
	// 先登记中断能力，再检查领取与开始执行之间发生的持久取消。
	current, err := s.repo.GetJob(ctx, job.Owner, job.ID)
	if err != nil {
		return 0, err
	}
	if current.Status != "running" || current.Generation != job.Generation {
		return 0, context.Canceled
	}
	if err = ctx.Err(); err != nil {
		return 0, err
	}
	return handler(ctx, job)
}
