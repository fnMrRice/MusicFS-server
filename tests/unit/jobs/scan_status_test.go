package jobs_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

type stoppedStatusRepository struct {
	jobs.Repository
	job *jobs.Job
}

func (*stoppedStatusRepository) RecoverJobs(context.Context) error { return nil }
func (*stoppedStatusRepository) Claim(context.Context) (jobs.Job, error) {
	return jobs.Job{}, errors.New("private storage failure")
}
func (r *stoppedStatusRepository) ScanStatus(context.Context, string, string) (jobs.ScanStatus, error) {
	return jobs.ScanStatus{Job: r.job}, nil
}

func TestScanStatusReportsStoppedSchedulerInsteadOfStaleRunningTask(t *testing.T) {
	repo := &stoppedStatusRepository{job: &jobs.Job{ID: "task", Status: "running"}}
	service := jobs.New(repo)
	ctx := context.Background()
	if err := service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	deadline := time.Now().Add(2 * time.Second)
	for service.Err() == nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if service.Err() == nil {
		t.Fatal("scheduler failure was not recorded")
	}
	if _, err := service.ScanStatus(ctx, "admin", "local"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("stopped scheduler exposed a stale task or private failure", err)
	}
	repo.job.Status = "succeeded"
	if status, err := service.ScanStatus(ctx, "admin", "local"); err != nil || status.Job == nil || status.Job.Status != "succeeded" {
		t.Fatal("completed history became unavailable", status, err)
	}
	repo.job = nil
	if status, err := service.ScanStatus(ctx, "admin", "local"); err != nil || status.Job != nil {
		t.Fatal("empty scan history became unavailable", status, err)
	}
}
