package bootstrap

import (
	"context"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/src/tasks"
)

func libraryTaskExecutor(store storage.Backend, scrapers *onlinescrape.Service) tasks.ExecuteOperation {
	return func(ctx context.Context, op tasks.Operation, report tasks.ReportProgress) (tasks.Progress, tasks.Result, error) {
		switch op.Task.Kind {
		case tasks.AlbumAutoScrape:
			var input tasks.AlbumScrapeRequest
			if err := json.Unmarshal(op.Payload, &input); err != nil {
				return tasks.Progress{}, tasks.Result{}, err
			}
			counts := func(batch onlinescrape.AlbumBatchProgress) (tasks.Progress, tasks.Result) {
				return tasks.Progress{Total: batch.Total, Completed: batch.Processed},
					tasks.Result{Updated: batch.Saved, Retained: batch.Skipped, NotFound: batch.NotFound, Failed: batch.Failed}
			}
			batch, err := scrapers.AutoScrapeAlbums(ctx, store, op.Task.OwnerID, input.ProviderInstanceID, input.Q,
				func(batch onlinescrape.AlbumBatchProgress) error { return report(counts(batch)) })
			progress, result := counts(batch)
			return progress, result, err
		case tasks.BulkMerge:
			var input matching.BulkMergeRequest
			if err := json.Unmarshal(op.Payload, &input); err != nil {
				return tasks.Progress{}, tasks.Result{}, err
			}
			merged, err := store.BulkMergeSummary(ctx, op.Task.OwnerID, input, op.Task.ID)
			if err != nil {
				return tasks.Progress{}, tasks.Result{}, err
			}
			return tasks.Progress{Total: 1, Completed: 1},
				tasks.Result{MergedGroups: merged.MergedGroups, MergedSources: merged.MergedSources, SkippedGroups: merged.SkippedGroups}, nil
		default:
			return tasks.Progress{}, tasks.Result{}, domain.Fail("UNSUPPORTED", "Unknown library task kind")
		}
	}
}
