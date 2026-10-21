package mediastore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore"
)

func TestContentRecoveryAcrossEveryPublicationBoundary(t *testing.T) {
	for _, stage := range []string{"prepared", "partial", "written", "renamed", "published", "referenced", "finished", "damaged-reference"} {
		t.Run(stage, func(t *testing.T) {
			f := setup(t)
			ctx := context.Background()
			old := upload(t, f, "last valid manual content")
			if _, err := f.service.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: f.metadata.Target, ObjectID: old.ID}, domain.NewID()); err != nil {
				t.Fatal(err)
			}
			body := "next content awaiting selection"
			h := sha256.Sum256([]byte(body))
			meta := f.metadata
			meta.Format = "text"
			p, _, err := f.store.PrepareContent(ctx, "admin", mediastore.UploadInput{Metadata: meta, MIME: "text/plain; charset=utf-8", Size: int64(len(body)), SHA256: hex.EncodeToString(h[:])}, domain.NewID())
			if err != nil {
				t.Fatal(err)
			}
			if stage == "partial" {
				if err = os.WriteFile(filepath.Join(f.root, "staging", p.Object.ID), []byte("half"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if stage != "prepared" && stage != "partial" {
				p.Object, err = f.files.Write(ctx, p.Object.ID, "lyrics", p.Object.MIME, strings.NewReader(body), int64(len(body)), func(r io.ReadSeeker) error {
					raw, err := io.ReadAll(r)
					if err != nil {
						return err
					}
					if string(raw) != body {
						return io.ErrUnexpectedEOF
					}
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if err = f.store.WrittenPublication(ctx, "admin", p.ID, p.Object); err != nil {
					t.Fatal(err)
				}
				if stage != "written" {
					if err = f.files.Publish(p.Object); err != nil {
						t.Fatal(err)
					}
					if stage != "renamed" {
						if err = f.store.SetPublicationStage(ctx, "admin", p.ID, "written", "published"); err != nil {
							t.Fatal(err)
						}
						if stage != "published" {
							if err = f.store.ReferencePublication(ctx, "admin", p.ID); err != nil {
								t.Fatal(err)
							}
							if stage == "finished" {
								if err = f.store.SetPublicationStage(ctx, "admin", p.ID, "referenced", "finished"); err != nil {
									t.Fatal(err)
								}
							}
							if stage == "damaged-reference" {
								if err = os.WriteFile(filepath.Join(f.root, p.Object.RelativePath), []byte(body+"damage"), 0600); err != nil {
									t.Fatal(err)
								}
							}
						}
					}
				}
			}
			var seq int
			var name, path string
			if err = f.store.DB.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
				t.Fatal(err)
			}
			if err = f.store.Close(); err != nil {
				t.Fatal(err)
			}
			if err = f.files.Close(); err != nil {
				t.Fatal(err)
			}
			store, err := sqlite.Open(ctx, path)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			files, err := mediastore.OpenFiles(f.root)
			if err != nil {
				t.Fatal(err)
			}
			defer files.Close()
			service, err := mediastore.New(store, files)
			if err != nil {
				t.Fatal(err)
			}
			if err = service.Recover(ctx); err != nil {
				t.Fatal(err)
			}
			if err = service.Recover(ctx); err != nil {
				t.Fatal("recovery not idempotent", err)
			}
			content := mediastore.NewContent(store, store, files)
			selection, err := store.ContentSelection(ctx, "admin", meta.Target.SourceTrackID)
			if err != nil || len(selection.Bindings) != 1 || selection.Bindings[0].ObjectID != old.ID || !selection.Bindings[0].ManualLocked {
				t.Fatal("prior manual selection lost", err)
			}
			_, in, err := content.Open(ctx, "admin", old.ID)
			if err != nil {
				t.Fatal(err)
			}
			in.Close()
			recovered, err := store.GetPublication(ctx, "admin", p.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := "aborted"
			if stage == "referenced" || stage == "finished" {
				want = "finished"
			}
			if stage == "damaged-reference" {
				want = "unavailable"
			}
			if recovered.Status != want {
				t.Fatalf("recovered to %s, want %s", recovered.Status, want)
			}
			if _, err = os.Stat(filepath.Join(f.root, "staging", p.Object.ID)); !os.IsNotExist(err) {
				t.Fatal("incomplete staging was not reclaimed")
			}
		})
	}
}
