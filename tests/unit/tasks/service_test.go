package tasks_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

type taskRepository struct {
	tasks.Repository
	mu          sync.Mutex
	task        tasks.Task
	completed   chan struct{}
	finishCount int
}

func (r *taskRepository) CancelRefreshTask(context.Context, string, string) (tasks.Task, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.task.Status, r.task.CancelRequested = "cancelling", true
	return r.task, nil
}
func (r *taskRepository) RefreshTaskMetadataJobs(context.Context, string, string) ([]string, error) {
	return []string{"metadata"}, nil
}
func (r *taskRepository) CompleteRefreshTaskCancellation(context.Context, string, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finishCount++
	r.task.Status = "cancelled"
	close(r.completed)
	return nil
}

type interrupters struct {
	scan, metadata chan struct{}
	metadataCalled chan []string
}

func (i *interrupters) InterruptJob(string, string) <-chan struct{} { return i.scan }
func (i *interrupters) InterruptJobs(ids []string) <-chan struct{} {
	i.metadataCalled <- ids
	return i.metadata
}

func TestTaskCancellationWaitsForResourcesAndOutlivesRequest(t *testing.T) {
	repo := &taskRepository{task: tasks.Task{ID: "refresh", OwnerID: "owner", ProviderInstanceID: "provider", RootIDs: []string{"root"}}, completed: make(chan struct{})}
	interrupt := &interrupters{scan: make(chan struct{}), metadata: make(chan struct{}), metadataCalled: make(chan []string, 1)}
	service := tasks.New(repo, interrupt, interrupt, func(string, string, []string) (string, []tasks.Root) {
		return "Local music", []tasks.Root{{ID: "root", Name: "Music"}}
	})
	defer service.Close()
	ctx, cancel := context.WithCancel(context.Background())
	task, err := service.Cancel(ctx, "owner", "refresh")
	cancel()
	if err != nil || task.Status != "cancelling" || task.OwnerName != "owner" || task.ProviderName != "Local music" || task.Roots[0].Name != "Music" {
		t.Fatal("invalid cancelling task", task, err)
	}
	if _, err := service.Cancel(context.Background(), "owner", "refresh"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.completed:
		t.Fatal("cancellation completed while scan owns resources")
	default:
	}
	close(interrupt.scan)
	select {
	case ids := <-interrupt.metadataCalled:
		if !reflect.DeepEqual(ids, []string{"metadata"}) {
			t.Fatal("wrong metadata tasks interrupted", ids)
		}
	case <-time.After(time.Second):
		t.Fatal("request cancellation stopped durable cleanup")
	}
	select {
	case <-repo.completed:
		t.Fatal("cancellation completed while metadata owns resources")
	default:
	}
	close(interrupt.metadata)
	select {
	case <-repo.completed:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not finish")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.finishCount != 1 {
		t.Fatal("duplicate cancellation started duplicate cleanup")
	}
}
