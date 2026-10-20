package scraperlocal_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func FuzzScraperMalformedMetadata(f *testing.F) {
	f.Add(testkit.ScraperID3(4, testkit.ScraperFrame{ID: "USLT", Body: []byte("\x03eng\x00lyrics")}))
	f.Add(testkit.ScraperFLAC(testkit.ScraperBlock{Kind: 4, Body: testkit.ScraperVorbis("LYRICS=[00:01]test")}))
	f.Add([]byte("ID3\x04\x00\x00\x7f\x7f\x7f\x7f"))
	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) == 0 || len(body) > 1<<20 {
			return
		}
		request := enrichment.SearchRequest{Context: pluginproto.Context{UserID: "u", InstanceID: "i", TaskID: "t", TimeoutMs: 1000}, Target: enrichment.Target{SourceTrackID: "s", AssetID: "a", AssetVersion: "v"}, Fields: []string{"lyrics", "artwork"}, Resources: []enrichment.Resource{{ResourceRef: strings.Repeat("a", 48), Version: "v", Kind: "audio", Match: "embedded", SizeBytes: int64(len(body))}}}
		engine, err := scraperlocal.NewEngine(request, func(context.Context, enrichment.Resource) (io.ReaderAt, error) { return bytes.NewReader(body), nil })
		if err != nil {
			t.Fatal(err)
		}
		result, err := engine.Search(context.Background())
		if err != nil {
			return
		}
		if len(result.Candidates) > 64 {
			t.Fatal("candidate bound exceeded")
		}
		for _, candidate := range result.Candidates {
			if candidate.Status == "available" {
				if _, err := engine.Fetch(context.Background(), candidate.CandidateID, io.Discard); err != nil {
					t.Fatal(err)
				}
			}
		}
	})
}
