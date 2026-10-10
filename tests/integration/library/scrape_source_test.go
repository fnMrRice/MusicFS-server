package library_test

import (
	"bytes"
	"context"
	"crypto/sha512"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/identification"
	"github.com/fnMrRice/MusicFS-server/src/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/pluginhost"
	"github.com/fnMrRice/MusicFS-server/src/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/plugins/scraperlocal"
	"github.com/fnMrRice/MusicFS-server/src/sourceaccess"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type scrapeRepository struct {
	library.Repository
	track   library.Track
	changed atomic.Bool
}

func (r *scrapeRepository) FindTrack(_ context.Context, owner, id string) (library.Track, error) {
	if owner != r.track.Owner || id != r.track.ID {
		return library.Track{}, domain.Fail("NOT_FOUND", "Source not found")
	}
	t := r.track
	if r.changed.Load() {
		t.AssetID = "replacement"
	}
	return t, nil
}

func scrapeFixture(t *testing.T, name string, audio []byte) (*library.Service, *scrapeRepository, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), audio, 0600); err != nil {
		t.Fatal(err)
	}
	stem := name[:len(name)-len(filepath.Ext(name))]
	if err := os.WriteFile(filepath.Join(root, stem+".lrc"), []byte("[00:01]旁置歌词"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cover.png"), testkit.ScraperPNG(), 0600); err != nil {
		t.Fatal(err)
	}
	access, err := sourceaccess.New([]sourceaccess.RootConfig{{ID: "root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	entry, err := access.Inspect("root", name)
	if err != nil {
		t.Fatal(err)
	}
	title := "Synthetic track"
	repo := &scrapeRepository{track: library.Track{ID: "track", Owner: "owner", ProviderID: "provider", AssetID: "asset", RootID: "root", Path: name, Version: entry.ObservedVersion, Size: entry.SizeBytes, MIME: entry.MIME, Availability: "available", Title: &title, Artists: []string{"Synthetic artist"}, Asset: &identification.Asset{ID: "asset", Version: entry.ObservedVersion, Current: true, FileSizeBytes: &entry.SizeBytes}}}
	service := library.New(repo, jobs.New(nil), []library.Runtime{{ID: "provider", Owner: "owner", Worker: &pluginhost.Worker{Access: access}}}, nil)
	return service, repo, root
}

func scrapeSearch(t *testing.T, input *library.ScrapeInput) enrichment.SearchResult {
	t.Helper()
	resources := []enrichment.Resource{}
	for _, value := range input.Inputs {
		resources = append(resources, value.Resource)
	}
	request := enrichment.SearchRequest{Context: pluginproto.Context{UserID: "owner", InstanceID: "scraper", TaskID: "task", TimeoutMs: 30000}, Target: input.Target, Fields: []string{"lyrics", "artwork"}, Languages: []string{}, Resources: resources}
	engine, err := scraperlocal.NewEngine(request, func(_ context.Context, resource enrichment.Resource) (io.ReaderAt, error) {
		for _, value := range input.Inputs {
			if value.Resource.ResourceRef == resource.ResourceRef {
				return value.Reader, value.Validate()
			}
		}
		return nil, os.ErrNotExist
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Search(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestScrapeSourceAuthorizationStablePublicationAndClose(t *testing.T) {
	audio := testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00Embedded lyrics")})
	service, repo, root := scrapeFixture(t, "track.mp3", audio)
	if _, err := service.OpenScrape(context.Background(), "another", "task", "track", []string{"lyrics"}); err == nil {
		t.Fatal("cross-owner source opened")
	}
	input, err := service.OpenScrape(context.Background(), "owner", "task", "track", []string{"lyrics", "artwork"})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	input.Target.Revision = 9
	if len(input.Inputs) != 3 || len(input.Issues) != 0 || input.Encrypted || input.Query.Title == "" {
		t.Fatal("incomplete protected inputs")
	}
	result := scrapeSearch(t, input)
	if len(result.Candidates) != 3 {
		t.Fatalf("missing source candidates: %+v", result)
	}
	for _, candidate := range result.Candidates {
		if candidate.Target.Revision != 9 {
			t.Fatal("selection revision lost")
		}
	}
	published := false
	if err = input.Publish(func() error { published = true; return nil }); err != nil || !published {
		t.Fatal("protected reference failed", err)
	}
	repo.changed.Store(true)
	published = false
	if err = input.Publish(func() error { published = true; return nil }); err == nil || published {
		t.Fatal("stale DB asset published")
	}
	repo.changed.Store(false)
	if err = input.Close(); err != nil {
		t.Fatal(err)
	}
	if err = input.Validate(); err == nil {
		t.Fatal("closed source remains valid")
	}
	after, err := os.ReadFile(filepath.Join(root, "track.mp3"))
	if err != nil || sha512.Sum512(after) != sha512.Sum512(audio) {
		t.Fatal("original changed")
	}
}

func TestScrapeSourceMissingNCMDecoderRetainsSidecarsAndCancellation(t *testing.T) {
	audio, err := testkit.BuildNCM(testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00Embedded lyrics")}), nil)
	if err != nil {
		t.Fatal(err)
	}
	service, _, _ := scrapeFixture(t, "track.ncm", audio)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, err := service.OpenScrape(ctx, "owner", "task", "track", []string{"lyrics", "artwork"})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if !input.Encrypted {
		t.Fatal("NCM not identified")
	}
	if err = input.DecodeNCM(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if len(input.Inputs) != 2 || len(input.Issues) != 1 || input.Issues[0].Code != "UNSUPPORTED" {
		t.Fatal("NCM missing decoder discarded independent sidecars")
	}
	if len(scrapeSearch(t, input).Candidates) != 2 {
		t.Fatal("sidecars unavailable without decoder")
	}
	cancel()
	if err = input.Validate(); err == nil {
		t.Fatal("cancelled source remains valid")
	}
	if err = input.Publish(func() error { t.Fatal("cancelled source published"); return nil }); err == nil {
		t.Fatal("cancelled publication succeeded")
	}
}

func TestScrapeSourceHardLinkedCoversRetainIndependentProtection(t *testing.T) {
	service, _, root := scrapeFixture(t, "track.mp3", []byte("ID3-no-embedded-metadata"))
	if err := os.Link(filepath.Join(root, "cover.png"), filepath.Join(root, "folder.png")); err != nil {
		t.Skip("host cannot create hard links")
	}
	input, err := service.OpenScrape(context.Background(), "owner", "task", "track", []string{"artwork"})
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if len(input.Inputs) != 3 || len(input.Issues) != 0 {
		t.Fatalf("hard-linked sibling guards failed: %d %+v", len(input.Inputs), input.Issues)
	}
	if err = input.Publish(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	first, _ := io.ReadAll(io.NewSectionReader(input.Inputs[1].Reader, 0, input.Inputs[1].Resource.SizeBytes))
	second, _ := io.ReadAll(io.NewSectionReader(input.Inputs[2].Reader, 0, input.Inputs[2].Resource.SizeBytes))
	if !bytes.Equal(first, second) {
		t.Fatal("hard-linked content changed")
	}
}
