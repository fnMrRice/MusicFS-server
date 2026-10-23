package scraperlocal_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/storage/mediastore/content"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestContainerArtworkKeepsIndependentProvenanceAndFailures(t *testing.T) {
	picture := testkit.ScraperPNG()
	audio := testkit.ScraperFLAC(testkit.ScraperBlock{Kind: 6, Body: testkit.ScraperFLACPicture(picture, "image/png")})
	bodies := [][]byte{audio, picture, picture, picture[:len(picture)-8], picture}
	resources := make([]enrichment.Resource, len(bodies))
	for i, body := range bodies {
		resources[i] = enrichment.Resource{ResourceRef: fmt.Sprintf("%048x", i+1), Version: "source-version", Kind: "artwork", Match: "containerEmbedded", SizeBytes: int64(len(body))}
	}
	resources[0].Kind, resources[0].Match = "audio", "embedded"
	resources[1].Match = "exactStem"
	resources[4].SizeBytes = content.MaxArtworkBytes + 1
	request := enrichment.SearchRequest{
		Context: pluginproto.Context{UserID: "user", InstanceID: "scraper", TaskID: "task", TimeoutMs: 30000},
		Target:  enrichment.Target{SourceTrackID: "track", AssetID: "asset", AssetVersion: "source-version"},
		Fields:  []string{"artwork"}, Resources: resources,
	}
	engine, err := scraperlocal.NewEngine(request, func(_ context.Context, resource enrichment.Resource) (io.ReaderAt, error) {
		for i, descriptor := range resources {
			if resource.ResourceRef == descriptor.ResourceRef {
				return bytes.NewReader(bodies[i]), nil
			}
		}
		return nil, fmt.Errorf("NOT_FOUND: resource unavailable")
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := engine.Search(context.Background())
	if err != nil || len(result.Candidates) != len(resources) {
		t.Fatalf("independent artwork candidates missing: %+v %v", result, err)
	}
	wantOrigins := []string{"embedded", "sidecar", "container", "container", "container"}
	wantBases := []string{"PICTURE", "exactStem", "containerEmbedded", "containerEmbedded", "containerEmbedded"}
	for i, candidate := range result.Candidates {
		if candidate.Origin != wantOrigins[i] || candidate.Basis != wantBases[i] || candidate.ResourceRef != resources[i].ResourceRef || candidate.ResourceVersion != resources[i].Version || candidate.Target != request.Target {
			t.Fatalf("candidate provenance changed: %+v", candidate)
		}
		var output bytes.Buffer
		_, fetchErr := engine.Fetch(context.Background(), candidate.CandidateID, &output)
		if i < 3 {
			if candidate.Status != "available" || fetchErr != nil || !bytes.Equal(output.Bytes(), picture) {
				t.Fatalf("valid image not preserved: %+v %v", candidate, fetchErr)
			}
		} else if candidate.Status != "unavailable" || candidate.FailureCode == nil || *candidate.FailureCode != "UNSUPPORTED" || candidate.SizeBytes != 0 || fetchErr == nil || output.Len() != 0 {
			t.Fatalf("damaged or oversized image available: %+v %v", candidate, fetchErr)
		}
	}
}

func TestContainerResourceAuthorizationAndSharedBudget(t *testing.T) {
	base := enrichment.SearchRequest{
		Context: pluginproto.Context{UserID: "user", InstanceID: "scraper", TaskID: "task", TimeoutMs: 30000},
		Target:  enrichment.Target{SourceTrackID: "track", AssetID: "asset", AssetVersion: "version"},
		Fields:  []string{"lyrics", "artwork"},
	}
	for _, tc := range []struct {
		kind, match string
		allowed     bool
	}{
		{"artwork", "containerEmbedded", true},
		{"lyrics", "containerEmbedded", false},
		{"audio", "containerEmbedded", false},
		{"artwork", "embedded", false},
	} {
		t.Run(tc.kind+"/"+tc.match, func(t *testing.T) {
			request := base
			request.Resources = []enrichment.Resource{{ResourceRef: strings.Repeat("a", 48), Version: "version", Kind: tc.kind, Match: tc.match, SizeBytes: 1}}
			if err := request.Validate(); (err == nil) != tc.allowed {
				t.Fatalf("unexpected authorization: %v", err)
			}
		})
	}
	base.Resources = []enrichment.Resource{
		{ResourceRef: strings.Repeat("a", 48), Version: "version", Kind: "artwork", Match: "containerEmbedded", SizeBytes: 8 << 20},
		{ResourceRef: strings.Repeat("b", 48), Version: "version", Kind: "artwork", Match: "exactStem", SizeBytes: 24 << 20},
	}
	if err := base.Validate(); err != nil {
		t.Fatal("shared attachment budget boundary rejected", err)
	}
	base.Resources[1].SizeBytes++
	if err := base.Validate(); err == nil {
		t.Fatal("container artwork bypassed the shared input budget")
	}
	base.Resources = base.Resources[:1]
	base.Fields = []string{"lyrics"}
	if err := base.Validate(); err == nil {
		t.Fatal("unrequested container artwork was authorized")
	}
}
