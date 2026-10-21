package storage_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/metadata/identification/fingerprint"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestFingerprintFileAndDatabaseRecoveryAtEachPublicationBoundary(t *testing.T) {
	for _, stage := range []string{"prepared", "unrecorded-write", "written", "unrecorded-rename", "published", "referenced", "missing-reference", "corrupt-reference"} {
		t.Run(stage, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			dbPath := filepath.Join(root, "data.db")
			mediaPath := filepath.Join(root, "media")
			s, err := sqlite.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { s.Close() }()
			files, err := mediastore.OpenFiles(mediaPath)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { files.Close() }()
			track := publishEvidenceTracks(t, s, evidenceObservation("track", "v1"))[0]
			old := fpClaim(t, s, fpTarget(track))
			p, raw := fpPublication(t, old)
			boundary := stage
			switch stage {
			case "unrecorded-write":
				boundary = "prepared"
			case "unrecorded-rename":
				boundary = "written"
			case "missing-reference", "corrupt-reference":
				boundary = "referenced"
			}
			p = fpAdvance(t, s, files, p, raw, boundary)
			if stage == "unrecorded-write" {
				if _, err = files.Write(ctx, p.Object.ID, p.Kind, p.Object.MIME, bytes.NewReader(raw), fingerprint.MaxDetailBytes, fpValidator); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "unrecorded-rename" {
				if err = files.Publish(p.Object); err != nil {
					t.Fatal(err)
				}
			}
			if err = files.Close(); err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			objectPath := filepath.Join(mediaPath, "objects", p.Object.ID)
			if stage == "missing-reference" {
				if err = os.Remove(objectPath); err != nil {
					t.Fatal(err)
				}
			}
			if stage == "corrupt-reference" {
				if err = os.WriteFile(objectPath, []byte("corrupt"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			s, err = sqlite.Open(ctx, dbPath)
			if err != nil {
				t.Fatal(err)
			}
			files, err = mediastore.OpenFiles(mediaPath)
			if err != nil {
				t.Fatal(err)
			}
			service, err := mediastore.New(s, files)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.Recover(ctx); err != nil {
				t.Fatal("media recovery blocked startup", err)
			}
			if err = s.RecoverFingerprints(ctx); err != nil {
				t.Fatal(err)
			}
			recovered, err := s.GetPublication(ctx, "admin", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			job, err := s.GetFingerprintJob(ctx, "admin", old.ID)
			if err != nil {
				t.Fatal(err)
			}
			switch stage {
			case "referenced":
				if recovered.Status != "finished" || job.Status != "succeeded" || job.Result == nil || !job.Result.Current {
					t.Fatal("committed reference was not recovered")
				}
			case "missing-reference", "corrupt-reference":
				if recovered.Status != "unavailable" || job.Status != "unavailable" || job.Result == nil || job.Result.Current || job.Error.Code != "RESOURCE_CHANGED" {
					t.Fatal("lost file remained successful evidence")
				}
			default:
				if recovered.Status != "aborted" || job.Status != "queued" || job.Generation <= old.Generation || job.Result != nil {
					t.Fatal("unguarded result was resumed as evidence")
				}
				if _, err = os.Stat(filepath.Join(mediaPath, "staging", p.Object.ID)); !os.IsNotExist(err) {
					t.Fatal("aborted staging bytes retained", err)
				}
				if stage == "published" || stage == "unrecorded-rename" {
					if _, err = os.Stat(objectPath); err != nil {
						t.Fatal("published orphan removed before reference-aware collection", err)
					}
				}
				if err = s.ReferencePublication(ctx, "admin", p.ID); err == nil {
					t.Fatal("recovered old publication was referenced")
				}
				if err = s.FailFingerprint(ctx, old, "TEMPORARY_FAILURE"); err == nil {
					t.Fatal("old execution remained active after restart")
				}
			}
			if err = service.Recover(ctx); err != nil {
				t.Fatal("recovery retry was not idempotent", err)
			}
		})
	}
}
