package library_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	ncmdecoder "github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScrapeNCMContainerArtworkIsIndependentAndTaskBound(t *testing.T) {
	plain, err := os.ReadFile(filepath.Join(testkit.RepositoryRoot(), "tests/testdata/audio/vbr-untagged.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	image := testkit.ScraperPNG()
	container, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{Cover: image, CoverPadding: 1234})
	if err != nil {
		t.Fatal(err)
	}
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"lyrics", "artwork"} {
		t.Run(field, func(t *testing.T) {
			service, repo, root := scrapeFixture(t, "track.ncm", container)
			input, err := service.OpenScrape(t.Context(), "owner", "task", "track", []string{field})
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			if err := input.DecodeAudio(t.Context(), decoder); err != nil || len(input.Issues) != 0 {
				t.Fatal("NCM resource preparation failed", err, input.Issues)
			}
			var attachment io.ReaderAt
			for _, resource := range input.Inputs {
				if resource.Resource.Match != "containerEmbedded" {
					continue
				}
				attachment = resource.Reader
				body, err := io.ReadAll(io.NewSectionReader(attachment, 0, resource.Resource.SizeBytes))
				if err != nil || !bytes.Equal(body, image) || resource.Resource.Version != input.Target.AssetVersion {
					t.Fatal("container artwork lost bytes or source version", err)
				}
			}
			if (attachment != nil) != (field == "artwork") {
				t.Fatal("container artwork did not respect requested fields")
			}
			if field == "artwork" {
				result := scrapeSearch(t, input)
				found := false
				for _, candidate := range result.Candidates {
					if candidate.Origin == "container" {
						found = candidate.Basis == "containerEmbedded" && candidate.Status == "available"
					}
				}
				if !found {
					t.Fatal("container artwork did not become an available candidate", result)
				}
			}
			count := len(input.Inputs)
			if err := input.DecodeAudio(t.Context(), decoder); err != nil || len(input.Inputs) != count {
				t.Fatal("repeated decode duplicated attachments", err)
			}
			repo.changed.Store(true)
			if err := input.Publish(func() error { t.Fatal("stale container published"); return nil }); err == nil {
				t.Fatal("container artwork bypassed source version guard")
			}
			if err := input.Close(); err != nil {
				t.Fatal(err)
			}
			if attachment != nil {
				if _, err := attachment.ReadAt(make([]byte, 1), 0); err == nil {
					t.Fatal("closed scrape retained container artwork access")
				}
			}
			after, err := os.ReadFile(filepath.Join(root, "track.ncm"))
			if err != nil || !bytes.Equal(after, container) {
				t.Fatal("artwork extraction modified original container", err)
			}
		})
	}
}

func TestScrapeNCMArtworkBudgetRetainsAudioAndSidecar(t *testing.T) {
	plain := []byte{0xff, 0xfb, 0x90, 0, 0, 0, 0, 0}
	container, err := testkit.BuildNCMWithOptions(plain, nil, testkit.NCMOptions{Cover: make([]byte, 32<<20)})
	if err != nil {
		t.Fatal(err)
	}
	service, _, _ := scrapeFixture(t, "track.ncm", container)
	input, err := service.OpenScrape(t.Context(), "owner", "task", "track", []string{"artwork"})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	decoder, err := ncmdecoder.NewProcessor(ncmdecoder.Config{Builtin: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := input.DecodeAudio(t.Context(), decoder); err != nil || len(input.Issues) != 1 || len(input.Inputs) != 2 {
		t.Fatal("oversized attachment discarded independent resources", err, input.Issues)
	}
	if input.Issues[0].Code != "UNSUPPORTED" {
		t.Fatal("oversized attachment was not reported", input.Issues)
	}
	actual, err := io.ReadAll(io.NewSectionReader(input.Inputs[0].Reader, 0, input.Inputs[0].Resource.SizeBytes))
	if err != nil || !bytes.Equal(actual, plain) {
		t.Fatal("oversized attachment damaged decoded audio", err)
	}
	result := scrapeSearch(t, input)
	if len(result.Candidates) != 1 || result.Candidates[0].Origin != "sidecar" || result.Candidates[0].Status != "available" {
		t.Fatal("oversized attachment hid valid directory artwork", result)
	}
}
