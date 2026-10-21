package enrichment_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
)

func TestMetadataJobValidationAndPublicProjection(t *testing.T) {
	valid := enrichment.JobRequest{SourceTrackIDs: []string{"track"}, Fields: []string{"lyrics"}, Languages: []string{"ZH-Hans"}, InstanceIDs: []string{"local"}}
	normal, err := valid.Normalize()
	if err != nil || normal.Languages[0] != "zh-hans" || normal.ExcludeInstanceIDs == nil {
		t.Fatal("normalization failed", err)
	}
	for _, change := range []func(*enrichment.JobRequest){func(r *enrichment.JobRequest) { r.SourceTrackIDs = nil }, func(r *enrichment.JobRequest) { r.SourceTrackIDs = []string{"same", "same"} }, func(r *enrichment.JobRequest) { r.SourceTrackIDs = make([]string, 65) }, func(r *enrichment.JobRequest) { r.Fields = []string{"lyrics", "lyrics"} }, func(r *enrichment.JobRequest) { r.ExcludeInstanceIDs = []string{"local"} }, func(r *enrichment.JobRequest) { r.Languages = []string{"zh", "ZH"} }} {
		request := valid
		change(&request)
		if _, err = request.Normalize(); err == nil {
			t.Fatal("invalid metadata request accepted")
		}
	}
	body, err := json.Marshal(enrichment.Job{Targets: []enrichment.JobTarget{{Candidates: []enrichment.PreviewCandidate{{CandidateID: "id"}}}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"resourceRef", "resourceVersion", "generation", "relativePath", "body"} {
		if strings.Contains(string(body), private) {
			t.Fatal("private field reached metadata job JSON")
		}
	}
}
