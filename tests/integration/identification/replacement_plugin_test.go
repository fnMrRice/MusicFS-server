package identification_test

import (
	"context"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprintworker"
)

func TestIndependentFingerprintPluginPersistsAndReloadsEvidence(t *testing.T) {
	store, source, media := setup(t)
	identity := fixtureIdentity("b")
	identity.Algorithm, identity.AlgorithmVersion, identity.AlgorithmConfig = "replacement-analyzer", "4.0.0", "native-words-v1"
	identity.Analyzer.Version = "replacement-native 4.0.0"
	identity.ConfigRevision = identity.Revision()
	worker, err := fingerprintworker.New(store, source, extractor{identity: identity}, media, fingerprintworker.Config{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = worker.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer worker.Close()
	job, _, err := worker.Submit(ctx, "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, worker, job.ID)
	if job.Status != "succeeded" || job.Result == nil {
		t.Fatalf("replacement plugin failed: %+v", job)
	}
	detail, err := worker.Detail(ctx, "admin", job.Result.ID)
	if err != nil || detail.Identity != identity || len(detail.Values) != 100 {
		t.Fatal("replacement evidence was not preserved", err)
	}
}
