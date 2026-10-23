package enrichment_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
)

type replacementScraper struct {
	query    atomic.Pointer[enrichment.Query]
	overflow atomic.Bool
}

func (*replacementScraper) Descriptor() enrichment.Descriptor {
	return enrichment.Descriptor{PluginID: "example.scraper.replacement", Fields: []string{"lyrics"}, QueryCorrectionsSupported: true}
}
func (*replacementScraper) PluginVersion() string { return "2.0.0" }
func (h *replacementScraper) Open(_ context.Context, _ pluginproto.Context, target enrichment.Target, inputs []enrichment.Input) (enrichment.Session, error) {
	return &replacementSession{host: h, target: target, input: inputs[0]}, nil
}

type replacementSession struct {
	host      *replacementScraper
	target    enrichment.Target
	input     enrichment.Input
	candidate enrichment.Candidate
}

const replacementLyrics = "Independent plugin lyrics"

func (*replacementSession) PluginVersion() string { return "2.0.0" }
func (*replacementSession) Close() error          { return nil }
func (s *replacementSession) Search(_ context.Context, _, _ []string, query *enrichment.Query) (enrichment.SearchResult, error) {
	s.host.query.Store(query)
	s.candidate = enrichment.Candidate{CandidateID: domain.NewID(), Target: s.target, Field: "lyrics", Language: "en", Format: "text", MIME: "text/plain; charset=utf-8", SizeBytes: int64(len(replacementLyrics)), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(replacementLyrics))), Origin: "replacement", Basis: "replacement", ResourceRef: s.input.Resource.ResourceRef, ResourceVersion: s.input.Resource.Version, Status: "available"}
	return enrichment.SearchResult{Candidates: []enrichment.Candidate{s.candidate}, Issues: []enrichment.Issue{}}, nil
}
func (s *replacementSession) Fetch(_ context.Context, _ string, dst io.Writer) (enrichment.Candidate, error) {
	body := replacementLyrics
	if s.host.overflow.Load() {
		body = strings.Repeat(body, 2)
	}
	_, err := io.WriteString(dst, body)
	return s.candidate, err
}

func TestReplacementScraperIdentityCapabilitiesQueryAndPublication(t *testing.T) {
	f := setup(t)
	plugin := &replacementScraper{}
	f.service.Close()
	var err error
	f.service, err = enrichmentworker.New(f.store, f.lib, f.content, []enrichmentworker.Runtime{{ID: "replacement", Name: "Replacement", Owner: "admin", Host: plugin}}, enrichmentworker.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.service.Close)
	ctx := context.Background()
	instances := f.service.Instances(ctx, "admin")
	if len(instances) != 1 || instances[0].PluginID != plugin.Descriptor().PluginID || !instances[0].QueryCorrectionsSupported || len(instances[0].Fields) != 1 {
		t.Fatalf("plugin capabilities were replaced: %+v", instances)
	}
	query := &enrichment.Query{Title: "Corrected title", Artists: []string{"Corrected artist"}}
	request := enrichment.JobRequest{SourceTrackIDs: []string{f.good().ID}, Fields: []string{"lyrics"}, Query: query, InstanceIDs: []string{"replacement"}}
	job, _, err := f.service.Submit(ctx, "admin", request, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Start(ctx); err != nil {
		t.Fatal(err)
	}
	job = finished(t, f, job.JobID)
	if job.State != "completed" || len(job.Targets[0].Candidates) != 1 || plugin.query.Load() == nil || plugin.query.Load().Title != query.Title {
		t.Fatalf("replacement scraper failed: %+v", job)
	}
	candidate := job.Targets[0].Candidates[0]
	if candidate.PreviewObjectID == nil {
		t.Fatal("replacement scraper did not publish a preview")
	}
	if enrichment.TrackSpecificCandidate(candidate) {
		t.Fatal("custom provenance must not implicitly enable automatic selection")
	}
	_, body, err := f.content.Open(ctx, "admin", *candidate.PreviewObjectID)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(data) != replacementLyrics {
		t.Fatal("replacement body changed", err)
	}
	documents, err := f.content.Derived().ListScrapeDocuments(ctx, "admin", job.JobID)
	if err != nil || len(documents) != 1 {
		t.Fatal("missing scraper trace", err)
	}
	_, body, err = f.content.Derived().Open(ctx, "admin", documents[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	var trace enrichment.ScrapeDocument
	if err = json.NewDecoder(body).Decode(&trace); err != nil || trace.Attempts[0].PluginID != plugin.Descriptor().PluginID {
		t.Fatal("trace used a different plugin identity", err)
	}
	request.Fields = []string{"artwork"}
	if _, _, err = f.service.Submit(ctx, "admin", request, domain.NewID()); err == nil {
		t.Fatal("undeclared scraper field was accepted")
	}

	plugin.overflow.Store(true)
	request.Fields = []string{"lyrics"}
	request.Query = &enrichment.Query{Title: "Oversized plugin response"}
	job, _, err = f.service.Submit(ctx, "admin", request, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	job = finished(t, f, job.JobID)
	candidate = job.Targets[0].Candidates[0]
	if candidate.Status != "unavailable" || candidate.PreviewObjectID != nil || candidate.FailureCode == nil || *candidate.FailureCode != "PROTOCOL_MISMATCH" {
		t.Fatal("replacement scraper bypassed preview limits", candidate)
	}
}
