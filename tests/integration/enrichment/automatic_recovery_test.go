package enrichment_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

type interruptedAction struct {
	trigger string
	action  enrichment.AutomationAction
}
type stopAfterAutomaticCommit struct {
	*sqlite.Store
	cancel  context.CancelFunc
	stopped chan interruptedAction
}

func (r *stopAfterAutomaticCommit) FinishAutomationAction(ctx context.Context, id string, a enrichment.AutomationAction) error {
	if a.Status == "applied" {
		select {
		case r.stopped <- interruptedAction{id, a}:
			r.cancel()
			return errors.New("injected interruption after automatic selection commit")
		default:
		}
	}
	return r.Store.FinishAutomationAction(ctx, id, a)
}

func TestAutomaticCommitRecoveryReplaysBeforeChangedSourceGuard(t *testing.T) {
	f := setup(t)
	j := rescanAutomatic(t, f, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &stopAfterAutomaticCommit{Store: f.store, cancel: cancel, stopped: make(chan interruptedAction, 1)}
	policy := enrichment.AutomaticPolicy{InstancePriority: []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}}
	service, err := enrichmentworker.New(repo, f.lib, f.content, f.runtimes, enrichmentworker.Config{Automatic: policy})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Close)
	f.service = service
	if err = service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	var interrupted interruptedAction
	select {
	case interrupted = <-repo.stopped:
	case <-time.After(20 * time.Second):
		t.Fatal("automatic apply did not commit")
	}
	service.Close()
	var source string
	if err = f.store.DB.QueryRow("SELECT source_id FROM metadata_automation WHERE id=?", interrupted.trigger).Scan(&source); err != nil {
		t.Fatal(err)
	}
	before, err := f.content.ContentSelection(context.Background(), "admin", source)
	if err != nil || before.Revision != 1 {
		t.Fatal("selection did not commit before interruption", before, err)
	}
	// The content commit is already successful. The interrupted action must
	// report that success without opening a newly modified source on replay.
	for _, track := range f.tracks {
		if track.ID == source {
			if err = os.WriteFile(filepath.Join(f.root, track.DisplayName), []byte("changed synthetic source"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	automaticService(t, &f, policy, f.runtimes)
	results := automaticFinished(t, f, j.ID)
	found := false
	for _, r := range results {
		if r.TriggerID == interrupted.trigger {
			for _, a := range r.Actions {
				if a.Field == interrupted.action.Field && a.Language == interrupted.action.Language {
					found = true
					if a.Status != "applied" || a.BindingID == nil || *a.BindingID != *interrupted.action.BindingID {
						t.Fatal("committed action was not replayed", a)
					}
				}
			}
		}
	}
	if !found {
		t.Fatal("recovered action missing")
	}
	after, err := f.content.ContentSelection(context.Background(), "admin", source)
	if err != nil || after.Revision != before.Revision {
		t.Fatal("recovery reselected content", after, err)
	}
}
