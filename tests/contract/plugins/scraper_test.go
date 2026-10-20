package plugins_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestScraperWireDTOsAndRestrictedChannel(t *testing.T) {
	ref := strings.Repeat("a", 48)
	target := enrichment.Target{SourceTrackID: "source", AssetID: "asset", AssetVersion: "version", Revision: 3}
	scope := pluginproto.Context{InstanceID: "16107a0a-24d2-5c96-8a8a-c7111a798bcf", UserID: "owner", TaskID: "task", TimeoutMs: 30000}
	request := enrichment.SearchRequest{Context: scope, Target: target, Fields: []string{"lyrics", "artwork"}, Languages: []string{}, Resources: []enrichment.Resource{{ResourceRef: ref, Version: "version", Kind: "lyrics", SizeBytes: 128, Match: "exactStem"}}}
	candidate := enrichment.Candidate{CandidateID: strings.Repeat("b", 48), Target: target, Field: "lyrics", Language: "und", Format: "lrc", MIME: "text/plain; charset=utf-8", SizeBytes: 128, SHA256: strings.Repeat("c", 64), Origin: "sidecar", ResourceRef: ref, ResourceVersion: "version", Basis: "exactStem", Status: "available"}
	result := enrichment.SearchResult{Candidates: []enrichment.Candidate{candidate}, Issues: []enrichment.Issue{}}
	fetch := enrichment.FetchRequest{Context: scope, Target: target, CandidateID: candidate.CandidateID, OutputRef: strings.Repeat("d", 48)}
	fetched := enrichment.FetchResult{Candidate: candidate, ContentRef: fetch.OutputRef}
	for name, value := range map[string]any{"ScrapeTarget": target, "ScrapeContext": scope, "ScrapeSearch": request, "ScrapeCandidate": candidate, "ScrapeSearchResult": result, "ScrapeFetch": fetch, "ScrapeFetchResult": fetched} {
		testkit.ValidatePlugin(t, name, value)
	}
	for method, value := range map[string]any{"scrape.search": request, "scrape.fetch": fetch} {
		testkit.ValidatePlugin(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": method, "params": value})
	}
	for _, value := range []any{result, fetched} {
		testkit.ValidatePlugin(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "result": value})
	}
	hello := map[string]any{"protocolVersion": "1.0", "instanceId": "16107a0a-24d2-5c96-8a8a-c7111a798bcf", "nonce": ref, "configRevision": "1", "context": scope, "target": target, "baseUrl": "http://127.0.0.1:12345/" + ref}
	testkit.ValidatePlugin(t, "ScrapeHello", hello)
	testkit.ValidatePlugin(t, "", map[string]any{"jsonrpc": "2.0", "id": "h-1", "method": "host.hello", "params": hello})
	for _, base := range []string{"https://example.invalid/" + ref, "http://localhost:12345/" + ref, "http://127.0.0.1:12345/" + ref + "?secret=x", "http://user@127.0.0.1:12345/" + ref} {
		hello["baseUrl"] = base
		assertScraperInvalid(t, "ScrapeHello", hello)
	}
	for _, mutate := range []func(map[string]any){
		func(v map[string]any) { v["body"] = "forbidden lyric text" },
		func(v map[string]any) { v["sourcePath"] = "private/music.mp3" },
		func(v map[string]any) { v["resources"] = nil },
		func(v map[string]any) { v["fields"] = []string{"lyrics", "lyrics"} },
		func(v map[string]any) {
			v["resources"] = []any{map[string]any{"resourceRef": "../../private", "version": "v", "kind": "lyrics", "sizeBytes": 1, "match": "exactStem"}}
		},
		func(v map[string]any) {
			v["context"] = map[string]any{"instanceId": "s", "userId": "o", "taskId": "t", "timeoutMs": 30001}
		},
	} {
		body, _ := json.Marshal(request)
		value := testkit.DecodeJSON(t, body).(map[string]any)
		mutate(value)
		assertScraperInvalid(t, "ScrapeSearch", value)
	}
	candidate.Status = "unavailable"
	code := "UNSUPPORTED"
	candidate.FailureCode = &code
	candidate.Format, candidate.MIME, candidate.SHA256, candidate.SizeBytes = "", "", "", 0
	testkit.ValidatePlugin(t, "ScrapeCandidate", candidate)
	candidate.SHA256 = strings.Repeat("c", 64)
	assertScraperInvalid(t, "ScrapeCandidate", candidate)
}

func assertScraperInvalid(t *testing.T, name string, value any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if testkit.ContractSchema(t, "plugin", name).Validate(testkit.DecodeJSON(t, body)) == nil {
		t.Fatalf("invalid scraper model accepted: %s", name)
	}
}
