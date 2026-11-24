package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

const (
	AlbumAutoScrape = "albumAutoScrape"
	BulkMerge       = "bulkMerge"
)

type AlbumScrapeRequest struct {
	ProviderInstanceID string `json:"providerInstanceId,omitempty"`
	Q                  string `json:"q,omitempty"`
}

type Operation struct {
	Task    Task
	Payload json.RawMessage
}

type OperationRepository interface {
	EnqueueOperation(context.Context, string, string, string, json.RawMessage) (Task, error)
	ClaimOperation(context.Context) (*Operation, error)
	UpdateOperation(context.Context, string, Progress, Result) error
	FinishOperation(context.Context, string, string, Progress, Result, *domain.Error) error
	RecoverOperations(context.Context) error
}

type ReportProgress func(Progress, Result) error
type ExecuteOperation func(context.Context, Operation, ReportProgress) (Progress, Result, error)

// OperationQueue 串行领取持久化的曲库操作；HTTP 请求结束不影响执行。
type OperationQueue struct {
	repo    OperationRepository
	execute ExecuteOperation
	changed func(string, string)
	wake    chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

func NewOperationQueue(repo OperationRepository, execute ExecuteOperation, changed func(string, string)) *OperationQueue {
	return &OperationQueue{repo: repo, execute: execute, changed: changed, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (q *OperationQueue) Start(ctx context.Context) error {
	if err := q.repo.RecoverOperations(ctx); err != nil {
		return err
	}
	run, cancel := context.WithCancel(ctx)
	q.cancel = cancel
	go q.work(run)
	return nil
}

func (q *OperationQueue) Close() {
	if q.cancel != nil {
		q.cancel()
		<-q.done
	}
}

func (q *OperationQueue) Enqueue(ctx context.Context, owner, kind, key string, payload json.RawMessage) (Task, error) {
	task, err := q.repo.EnqueueOperation(ctx, owner, kind, key, payload)
	// 提交后请求可能恰好断开，仍唤醒执行器检查队列。
	select {
	case q.wake <- struct{}{}:
	default:
	}
	if err == nil {
		q.changed(owner, task.ID)
	}
	return task, err
}

func (q *OperationQueue) work(ctx context.Context) {
	defer close(q.done)
	for ctx.Err() == nil {
		op, err := q.repo.ClaimOperation(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.WarnContext(ctx, "Could not claim library task", "error", err)
				waitForRetry(ctx)
			}
			continue
		}
		if op == nil {
			select {
			case <-ctx.Done():
			case <-q.wake:
			}
			continue
		}
		q.changed(op.Task.OwnerID, op.Task.ID)
		q.run(ctx, *op)
	}
}

func (q *OperationQueue) run(ctx context.Context, op Operation) {
	progress, result, err := q.execute(ctx, op, func(p Progress, r Result) error {
		if err := q.repo.UpdateOperation(ctx, op.Task.ID, p, r); err != nil {
			return err
		}
		q.changed(op.Task.OwnerID, op.Task.ID)
		return nil
	})
	status := "succeeded"
	var failure *domain.Error
	if err != nil {
		status = "failed"
		if !errors.As(err, &failure) {
			failure = &domain.Error{Code: "TEMPORARY_FAILURE", Message: "Could not complete the library task"}
		}
	}
	// 重试保存终态时不再执行业务操作。
	for ctx.Err() == nil {
		if err := q.repo.FinishOperation(ctx, op.Task.ID, status, progress, result, failure); err == nil {
			q.changed(op.Task.OwnerID, op.Task.ID)
			return
		} else {
			slog.WarnContext(ctx, "Could not finish library task", "taskId", op.Task.ID, "error", err)
		}
		waitForRetry(ctx)
	}
}

func waitForRetry(ctx context.Context) {
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
