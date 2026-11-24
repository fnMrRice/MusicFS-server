package tasks

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type ScanInterrupter interface {
	InterruptJob(string, string) <-chan struct{}
}
type MetadataInterrupter interface {
	InterruptJobs([]string) <-chan struct{}
}
type Names func(string, string, []string) (string, []Root)

type Service struct {
	// OnChanged 在注册服务前设置，通知持久化的取消请求及其收尾结果。
	OnChanged func(owner, taskID string)

	repo     Repository
	scans    ScanInterrupter
	metadata MetadataInterrupter
	names    Names
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	pending  map[string]bool
	wg       sync.WaitGroup
}

func New(repo Repository, scans ScanInterrupter, metadata MetadataInterrupter, names Names) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{repo: repo, scans: scans, metadata: metadata, names: names, ctx: ctx, cancel: cancel, pending: map[string]bool{}}
}

func (s *Service) List(ctx context.Context, owner, cursor string, limit int) (Page, error) {
	page, err := s.repo.ListRefreshTasks(ctx, owner, cursor, limit)
	for i := range page.Items {
		s.describe(&page.Items[i])
	}
	return page, err
}

func (s *Service) Get(ctx context.Context, owner, id string) (Task, error) {
	task, err := s.repo.GetRefreshTask(ctx, owner, id)
	s.describe(&task)
	return task, err
}

func (s *Service) Cancel(ctx context.Context, owner, id string) (Task, error) {
	task, err := s.repo.CancelRefreshTask(ctx, owner, id)
	if err != nil {
		return task, err
	}
	if task.CancelRequested && s.OnChanged != nil {
		s.OnChanged(task.OwnerID, task.ID)
	}
	if task.Status == "cancelling" {
		s.mu.Lock()
		if !s.pending[task.ID] && s.ctx.Err() == nil {
			s.pending[task.ID] = true
			s.wg.Add(1)
			go s.release(task)
		}
		s.mu.Unlock()
	}
	s.describe(&task)
	return task, nil
}

// 取消先在事务中撤销发布能力，再等待实际使用资源的协程退出。
// 请求断开不会撤回取消意图；服务退出后由持久恢复收尾。
func (s *Service) release(task Task) {
	defer s.wg.Done()
	defer func() { s.mu.Lock(); delete(s.pending, task.ID); s.mu.Unlock() }()
	if s.scans != nil && !s.wait(s.scans.InterruptJob(task.OwnerID, task.ID)) {
		return
	}
	for s.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		ids, err := s.repo.RefreshTaskMetadataJobs(ctx, task.OwnerID, task.ID)
		cancel()
		if err == nil {
			if s.metadata != nil && !s.wait(s.metadata.InterruptJobs(ids)) {
				return
			}
			ctx, cancel = context.WithTimeout(s.ctx, 5*time.Second)
			err = s.repo.CompleteRefreshTaskCancellation(ctx, task.OwnerID, task.ID)
			cancel()
			if err == nil {
				if s.OnChanged != nil {
					s.OnChanged(task.OwnerID, task.ID)
				}
				return
			}
		}
		slog.WarnContext(s.ctx, "Task cancellation completion requires retry", "taskId", task.ID)
		timer := time.NewTimer(time.Second)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) wait(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	case <-s.ctx.Done():
		return false
	}
}

func (s *Service) describe(task *Task) {
	task.OwnerName, task.ProviderName = task.OwnerID, task.ProviderInstanceID
	if task.Kind == AlbumAutoScrape || task.Kind == BulkMerge {
		return
	}
	task.Roots = make([]Root, 0, len(task.RootIDs))
	for _, id := range task.RootIDs {
		task.Roots = append(task.Roots, Root{ID: id, Name: id})
	}
	if s.names != nil {
		name, roots := s.names(task.OwnerID, task.ProviderInstanceID, task.RootIDs)
		if name != "" {
			task.ProviderName = name
		}
		if len(roots) == len(task.RootIDs) {
			task.Roots = roots
		}
	}
}

func (s *Service) Close() {
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
