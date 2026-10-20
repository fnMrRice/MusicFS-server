package identification_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/audio"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/identification/fingerprintworker"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type protectedSource struct {
	target      identification.HashTarget
	changed     atomic.Bool
	failPublish atomic.Bool
	opened      atomic.Int64
	closed      atomic.Int64
	published   atomic.Int64
}

func (s *protectedSource) ResolveHashTarget(_ context.Context, owner, id string) (identification.HashTarget, error) {
	if owner != "admin" || id != s.target.SourceTrackID {
		return identification.HashTarget{}, domain.Fail("NOT_FOUND", "Source not found")
	}
	return s.target, nil
}
func (s *protectedSource) AutoHashScopes() []identification.HashScope { return nil }
func (s *protectedSource) OpenStableHash(_ context.Context, owner, _ string, target identification.HashTarget) (identification.StableInput, error) {
	if owner != "admin" || target != s.target {
		return nil, domain.Fail("RESOURCE_CHANGED", "Source changed")
	}
	s.opened.Add(1)
	return &protectedInput{Reader: bytes.NewReader(bytes.Repeat([]byte{1}, 128)), source: s}, nil
}

type protectedInput struct {
	*bytes.Reader
	source *protectedSource
}

func (i *protectedInput) Validate() error {
	if i.source.changed.Load() {
		return domain.Fail("RESOURCE_CHANGED", "Source changed")
	}
	return nil
}
func (i *protectedInput) Publish(fn func() error) error {
	if i.source.failPublish.Load() {
		return domain.Fail("RESOURCE_CHANGED", "Source protection broke")
	}
	if err := i.Validate(); err != nil {
		return err
	}
	i.source.published.Add(1)
	return fn()
}
func (i *protectedInput) Close() error { i.source.closed.Add(1); return nil }

type extractor struct {
	identity fingerprint.Identity
	run      func(context.Context) error
}

func (e extractor) Identity() fingerprint.Identity { return e.identity }
func (e extractor) Extract(ctx context.Context, r fingerprint.Request, _ io.ReaderAt, _ int64) (fingerprint.Detail, error) {
	if e.run != nil {
		if err := e.run(ctx); err != nil {
			return fingerprint.Detail{}, err
		}
	}
	start, count, err := r.Samples()
	if err != nil {
		return fingerprint.Detail{}, err
	}
	values := make([]uint32, 100)
	for i := range values {
		values[i] = uint32(i*983 + 7)
	}
	return fingerprint.Detail{SchemaVersion: 1, Identity: e.identity, RequestedStartMs: r.StartMs, RequestedDurationMs: r.DurationMs, PCM: fingerprint.PCMInfo{Format: audio.FingerprintPCM, NormalizationRevision: audio.NormalizationRevision, StartSample: start, SampleCount: count, DecoderVersion: e.identity.Decoder.Version, DecoderSHA256: e.identity.Decoder.SHA256}, Quality: fingerprint.Assess(values, count), Values: values}, nil
}

func fixtureIdentity(decoderHash string) fingerprint.Identity {
	id := fingerprint.Identity{Algorithm: "chromaprint", AlgorithmVersion: "1.6.1", AlgorithmConfig: "cli-2-raw-v1", NormalizationRevision: audio.NormalizationRevision, QualityRevision: fingerprint.QualityRevision, Fpcalc: fingerprint.ToolIdentity{Version: "fpcalc version 1.6.1 (fixture)", SHA256: strings.Repeat("a", 64)}, Decoder: fingerprint.ToolIdentity{Version: "fixture-decoder", SHA256: strings.Repeat(decoderHash, 64)}}
	raw, _ := json.Marshal(id)
	sum := sha256.Sum256(raw)
	id.ConfigRevision = "sha256-" + hex.EncodeToString(sum[:])
	return id
}

func setup(t *testing.T) (*sqlite.Store, *protectedSource, *mediastore.Service) {
	t.Helper()
	ctx := context.Background()
	s, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "musicfs.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	_, _, err = s.Enqueue(ctx, "admin", "local", testkit.ScanPlan("root"), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	j, err := s.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := library.Observation{Observation: pluginproto.Observation{ExternalID: "sample", ObservedVersion: "v1", RootID: "root", RelativePath: "file.wav", SizeBytes: 128, MIME: "audio/wav", DisplayName: "Sample"}, Artists: []string{}}
	if err = s.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{o})); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(ctx, j, 1, nil); err != nil {
		t.Fatal(err)
	}
	page, err := s.ListTracks(ctx, "admin", "local", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	track := page.Items[0]
	source := &protectedSource{target: identification.HashTarget{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Asset.Version, FileSizeBytes: 128}}
	files, err := mediastore.OpenFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	media, err := mediastore.New(s, files)
	if err != nil {
		t.Fatal(err)
	}
	return s, source, media
}

func waitJob(t *testing.T, service *fingerprintworker.Service, id string) identification.FingerprintJob {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		j, err := service.Get(context.Background(), "admin", id)
		if err != nil {
			t.Fatal(err)
		}
		if j.Status != "running" && j.Status != "queued" {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("fingerprint job did not finish")
	return identification.FingerprintJob{}
}

func TestFingerprintWorkerPersistsDetailAndProtectsOriginalVersion(t *testing.T) {
	for _, mode := range []string{"success", "sourceChanged", "guardBroken", "extractorChanged"} {
		t.Run(mode, func(t *testing.T) {
			s, source, media := setup(t)
			e := extractor{identity: fixtureIdentity("b")}
			if mode == "sourceChanged" {
				e.run = func(context.Context) error { source.changed.Store(true); return nil }
			}
			if mode == "guardBroken" {
				source.failPublish.Store(true)
			}
			service, err := fingerprintworker.New(s, source, e, media, fingerprintworker.Config{MaxConcurrent: 1})
			if err != nil {
				t.Fatal(err)
			}
			var j identification.FingerprintJob
			if mode == "extractorChanged" {
				j, _, err = s.QueueFingerprint(context.Background(), "admin", identification.FingerprintTarget{HashTarget: source.target, FingerprintRequest: identification.FingerprintRequest{StartMs: 7, DurationMs: 30000, ExtractorIdentity: fixtureIdentity("c")}}, domain.NewID(), false)
			} else {
				j, _, err = service.Submit(context.Background(), "admin", source.target.SourceTrackID, domain.NewID(), 7, 30000)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err = service.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer service.Close()
			finished := waitJob(t, service, j.ID)
			if mode != "success" {
				if finished.Status != "superseded" || finished.Error == nil || finished.Error.Code != "RESOURCE_CHANGED" || finished.Result != nil {
					t.Fatalf("failed proof became valid: %+v", finished)
				}
				return
			}
			if finished.Status != "succeeded" || finished.Result == nil || !finished.Result.Current || finished.Result.ActualStartSample != 77 || source.published.Load() != 1 {
				t.Fatalf("invalid successful task: %+v", finished)
			}
			detail, err := service.Detail(context.Background(), "admin", finished.Result.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(detail.Values) != 100 || detail.Quality.Status != "available" {
				t.Fatal("persisted detail differs")
			}
			if _, err = service.Detail(context.Background(), "another-owner", finished.Result.ID); err == nil {
				t.Fatal("detail leaked across owners")
			}
			asset, err := s.Asset(context.Background(), "admin", source.target.AssetID)
			if err != nil {
				t.Fatal(err)
			}
			if asset.FileSHA512 != nil || asset.HashStatus != "unknown" {
				t.Fatal("fingerprint was presented as a complete original-file hash")
			}
			service.Close()
			if source.opened.Load() != source.closed.Load() {
				t.Fatal("protected input leaked")
			}
		})
	}
}

func TestFingerprintWorkerCancellationDoesNotPublish(t *testing.T) {
	s, source, media := setup(t)
	entered := make(chan struct{})
	e := extractor{identity: fixtureIdentity("b"), run: func(ctx context.Context) error { close(entered); <-ctx.Done(); return ctx.Err() }}
	service, err := fingerprintworker.New(s, source, e, media, fingerprintworker.Config{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	j, _, err := service.Submit(context.Background(), "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("extraction did not start")
	}
	if _, err = service.Cancel(context.Background(), "admin", j.ID); err != nil {
		t.Fatal(err)
	}
	service.Close()
	finished := waitJob(t, service, j.ID)
	if finished.Status != "cancelled" || finished.Result != nil || source.published.Load() != 0 || source.closed.Load() != 1 {
		t.Fatalf("cancellation leaked or published: %+v", finished)
	}
}

func TestFingerprintMissingDetailInvalidatesFinishedResultAndCanRecompute(t *testing.T) {
	s, source, media := setup(t)
	e := extractor{identity: fixtureIdentity("b")}
	service, err := fingerprintworker.New(s, source, e, media, fingerprintworker.Config{MaxConcurrent: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	j, _, err := service.Submit(context.Background(), "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
	if err != nil {
		t.Fatal(err)
	}
	completed := waitJob(t, service, j.ID)
	if completed.Result == nil {
		t.Fatal("result missing")
	}
	in, err := media.Open(completed.Result.Object)
	if err != nil {
		t.Fatal(err)
	}
	path := in.(*os.File).Name()
	in.Close()
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Detail(context.Background(), "admin", completed.Result.ID); err == nil {
		t.Fatal("missing detail was reported as readable")
	}
	failed, err := service.Get(context.Background(), "admin", j.ID)
	if err != nil || failed.Status != "unavailable" || failed.Result == nil || failed.Result.Current {
		t.Fatal("damaged published result remained valid", err)
	}
	retry, _, err := service.Submit(context.Background(), "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
	if err != nil {
		t.Fatal(err)
	}
	repaired := waitJob(t, service, retry.ID)
	if repaired.Status != "succeeded" || repaired.Result == nil || repaired.Result.Object.ID == completed.Result.Object.ID {
		t.Fatal("damaged media was silently reused")
	}
}

func TestFingerprintWorkerTotalDeadlineReleasesSource(t *testing.T) {
	s, source, media := setup(t)
	e := extractor{identity: fixtureIdentity("b"), run: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	service, err := fingerprintworker.New(s, source, e, media, fingerprintworker.Config{MaxConcurrent: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	started := time.Now()
	j, _, err := service.Submit(context.Background(), "admin", source.target.SourceTrackID, domain.NewID(), 0, 30000)
	if err != nil {
		t.Fatal(err)
	}
	finished := waitJob(t, service, j.ID)
	service.Close()
	if finished.Status != "failed" || finished.Error == nil || finished.Error.Code != "TEMPORARY_FAILURE" || finished.Result != nil || time.Since(started) > 4*time.Second || source.closed.Load() != 1 {
		t.Fatal("deadline did not bound extraction and release its source")
	}
}
