package jobs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
)

type loggingRepository struct{ finishRepository }

func (r *loggingRepository) Enqueue(context.Context, string, string, pluginproto.ScanPlan, string) (jobs.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.job, false, nil
}

func (r *loggingRepository) Finish(_ context.Context, _ jobs.Job, observed int, failure error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	decision := jobs.DecideRetry(failure, r.job.AttemptCount, time.Now())
	r.job.Status, r.job.Error, r.job.NextAttemptAt = decision.Status, decision.Error, decision.NextAttemptAt
	r.job.Progress.Observed = observed
	return nil
}

func TestScanLogsReflectDurableOutcomeWithoutPrivateDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, message, level string
		failure              error
	}{
		{"success", "Scan task completed", "INFO", nil},
		{"failure", "Scan task failed", "ERROR", errors.New("private/path credential=secret")},
		{"pause", "Scan task paused", "WARN", domain.Fail("AUTH_REQUIRED", "private/path credential=secret")},
		{"retry", "Scan task retry scheduled", "WARN", jobs.Retryable(domain.Fail("TEMPORARY_FAILURE", "private/path credential=secret"))},
		{"cancel", "Scan task cancelled", "INFO", context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			defer slog.SetDefault(previous)
			repo := &loggingRepository{finishRepository: finishRepository{job: jobs.Job{ID: "task", Owner: "admin", ProviderInstanceID: "local", Kind: "scan-full", Status: "queued", Generation: 1, AttemptCount: 1}}}
			service := jobs.New(repo)
			service.Register("scan-full", func(context.Context, jobs.Job) (int, error) { return 7, test.failure })
			if _, _, err := service.Submit(context.Background(), "admin", "local", pluginproto.ScanPlan{}, "operation-key"); err != nil {
				t.Fatal(err)
			}
			if err := service.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				job, _ := repo.GetJob(context.Background(), "admin", "task")
				if job.Status != "queued" && job.Status != "running" {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			service.Close()
			if strings.Contains(output.String(), "private") || strings.Contains(output.String(), "secret") {
				t.Fatal("Private diagnostics were written to logs")
			}
			seen := map[string]map[string]any{}
			for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
				var record map[string]any
				if err := json.Unmarshal(line, &record); err != nil {
					t.Fatal(err)
				}
				seen[record["msg"].(string)] = record
			}
			for _, message := range []string{"Scan task queued", "Scan task started", test.message} {
				if seen[message]["jobId"] != "task" || seen[message]["instanceId"] != "local" {
					t.Fatalf("Missing correlated lifecycle record: %s", message)
				}
			}
			if seen[test.message]["level"] != test.level || seen[test.message]["observed"] != float64(7) {
				t.Fatalf("Unexpected outcome record: %v", seen[test.message])
			}
			if test.name == "retry" && seen[test.message]["nextAttemptAt"] == nil {
				t.Fatal("Retry log omitted the persisted deadline")
			}
		})
	}
}
