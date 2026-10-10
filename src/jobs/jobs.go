package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

type Job struct {
	ID                 string `json:"jobId"`
	Owner              string `json:"-"`
	ProviderInstanceID string `json:"providerInstanceId"`
	Kind               string `json:"-"`
	Generation         int64  `json:"-"`
	Status             string `json:"status"`
	Progress           struct {
		Observed int `json:"observed"`
	} `json:"progress"`
	Complete      bool          `json:"complete"`
	Error         *domain.Error `json:"error"`
	LastSuccessAt *time.Time    `json:"lastSuccessAt"`
}
type Repository interface {
	Enqueue(context.Context, string, string, string, string) (Job, bool, error)
	Claim(context.Context) (Job, error)
	Finish(context.Context, Job, int, error) error
	GetJob(context.Context, string, string) (Job, error)
	RecoverJobs(context.Context) error
}
type Handler func(context.Context, Job) (int, error)
type Service struct {
	repo     Repository
	handlers map[string]Handler
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
}

func New(repo Repository) *Service                 { return &Service{repo: repo, handlers: map[string]Handler{}} }
func (s *Service) Register(kind string, h Handler) { s.handlers[kind] = h }
func (s *Service) Submit(ctx context.Context, user, target, kind, key string) (Job, bool, error) {
	return s.repo.Enqueue(ctx, user, target, kind, key)
}
func (s *Service) Get(ctx context.Context, user, id string) (Job, error) {
	return s.repo.GetJob(ctx, user, id)
}
func (s *Service) Start(ctx context.Context) error {
	if err := s.repo.RecoverJobs(ctx); err != nil {
		return err
	}
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	go func() {
		defer close(s.done)
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
			job, err := s.repo.Claim(ctx)
			if err != nil {
				continue
			}
			h := s.handlers[job.Kind]
			var n int
			if h == nil {
				err = errors.New("unsupported job type")
			} else {
				n, err = h(ctx, job)
			}
			finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.repo.Finish(finish, job, n, err)
			cancel()
		}
	}()
	return nil
}
func (s *Service) Close() {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
}
