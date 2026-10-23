package identification_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/plugins/plugincontrol"
)

func TestFingerprintUnavailableAnalyzerPreservesDependencyState(t *testing.T) {
	for _, mode := range []string{"missing", "enabled", "disabled", "failed"} {
		t.Run(mode, func(t *testing.T) {
			store, source, media := setup(t)
			worker, err := fingerprintworker.NewDormant(store, source, media, fingerprintworker.Config{MaxConcurrent: 1})
			if err != nil {
				t.Fatal(err)
			}
			var gate *plugincontrol.Gate
			want := "UNSUPPORTED"
			if mode != "missing" {
				gate = plugincontrol.NewGate(mode == "enabled")
			}
			if mode == "disabled" {
				want = "PLUGIN_DISABLED"
			}
			if mode == "failed" {
				want = "SOURCE_OFFLINE"
				gate.SetUnavailable(want, false)
			}
			worker.UpdateRuntime(nil, nil, gate)
			_, _, err = worker.Submit(t.Context(), "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
			var failure *domain.Error
			if !errors.As(err, &failure) || failure.Code != want {
				t.Fatalf("dependency %s reported %v, want %s", mode, err, want)
			}
			if gate != nil && gate.Active() != 0 {
				t.Fatal("unavailable analyzer leaked its audio lease")
			}
			if _, err = store.ClaimFingerprint(t.Context()); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("unavailable analyzer queued work", err)
			}
		})
	}
}

func TestFingerprintQueuedJobReportsMissingAnalyzerAfterRuntimeUpdate(t *testing.T) {
	store, source, media := setup(t)
	worker, err := fingerprintworker.New(store, source, extractor{identity: fixtureIdentity("b")}, media, fingerprintworker.Config{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	job, _, err := worker.Submit(t.Context(), "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
	if err != nil {
		t.Fatal(err)
	}
	gate := plugincontrol.NewGate(true)
	worker.UpdateRuntime(nil, nil, gate)
	if err = worker.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	finished := waitJob(t, worker, job.ID)
	if finished.Error == nil || finished.Error.Code != "UNSUPPORTED" || source.opened.Load() != 0 || gate.Active() != 0 {
		t.Fatalf("missing analyzer was misclassified or opened the source: %+v", finished)
	}
}
