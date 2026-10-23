package enrichmentworker

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

// InterruptJobs 在持久取消后中断插件和自动应用，返回所有已注册资源释放的通知。
func (s *Service) InterruptJobs(ids []string) <-chan struct{} {
	selected := map[string]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	var pending []<-chan struct{}
	s.mu.Lock()
	for _, running := range s.running {
		if selected[running.jobID] {
			running.cancel()
			pending = append(pending, running.done)
		}
	}
	s.mu.Unlock()
	done := make(chan struct{})
	go func() {
		for _, wait := range pending {
			<-wait
		}
		close(done)
	}()
	return done
}

func (s *Service) runAutomation(parent context.Context, repo enrichment.AutomationRepository, w enrichment.AutomationWork) (bool, error) {
	if w.JobID == nil {
		return s.automate(parent, repo, w)
	}
	ctx, cancel := context.WithCancel(parent)
	key := "automatic/" + w.TriggerID
	done := make(chan struct{})
	s.mu.Lock()
	s.running[key] = runningTask{jobID: *w.JobID, cancel: cancel, done: done}
	s.mu.Unlock()
	defer func() { cancel(); s.mu.Lock(); delete(s.running, key); close(done); s.mu.Unlock() }()
	return s.automate(ctx, repo, w)
}
