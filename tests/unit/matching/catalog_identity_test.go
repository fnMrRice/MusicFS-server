package matching_test

import (
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func TestCatalogRecordingIdentity(t *testing.T) {
	remote := matching.CatalogIdentity{NetEaseID: "42", Title: "Café", Album: "Album", Artists: []string{"Artist A", "Artist B"}, DurationMs: 180000}
	for _, tc := range []struct {
		name string
		edit func(*matching.CatalogIdentity)
		want bool
	}{
		{"same embedded identifier with different credits and artwork", func(s *matching.CatalogIdentity) { s.Album, s.Artists = "Reissue", []string{"Other credit"} }, true},
		{"different platform ID with identical tags", func(s *matching.CatalogIdentity) { s.NetEaseID = "43" }, false},
		{"implausible duration even with embedded ID", func(s *matching.CatalogIdentity) { s.DurationMs += 10000 }, false},
		{"same title album and complete credits across encodings", func(s *matching.CatalogIdentity) {
			s.NetEaseID, s.Title, s.Album, s.Artists, s.DurationMs = "", "  Cafe\u0301", "ALBUM", []string{"Artist B; Artist A"}, 181500
		}, true},
		{"different artist credit", func(s *matching.CatalogIdentity) { s.NetEaseID, s.Artists = "", []string{"Artist A"} }, false},
		{"group name is not split on ampersand", func(s *matching.CatalogIdentity) { s.NetEaseID, s.Artists = "", []string{"Artist A & Artist B"} }, false},
		{"same title different album", func(s *matching.CatalogIdentity) { s.NetEaseID, s.Album = "", "Compilation" }, false},
		{"explicit version", func(s *matching.CatalogIdentity) { s.NetEaseID, s.Title = "", "Café (Live)" }, false},
		{"missing album", func(s *matching.CatalogIdentity) { s.NetEaseID, s.Album = "", "" }, false},
		{"missing duration", func(s *matching.CatalogIdentity) { s.NetEaseID, s.DurationMs = "", 0 }, false},
		{"different length", func(s *matching.CatalogIdentity) { s.NetEaseID, s.DurationMs = "", 182001 }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := remote
			tc.edit(&local)
			if remote.Match(local) != tc.want || local.Match(remote) != tc.want {
				t.Fatalf("unexpected symmetric match for %+v", local)
			}
			if tc.want && !slices.ContainsFunc(local.Keys(), func(key string) bool { return slices.Contains(remote.Keys(), key) }) {
				t.Fatal("matching records have no common lookup key")
			}
		})
	}
}

func TestConflictingContainerAndEmbeddedIDsStayDistinctFromMissingEvidence(t *testing.T) {
	m := audiotypes.Metadata{}
	for _, id := range []string{"", "123", "123"} {
		m.AddNetEaseID(id)
	}
	if m.NetEaseID != "123" || m.NetEaseIDConflict {
		t.Fatalf("consistent evidence was rejected: %+v", m)
	}
	m.AddNetEaseID("456")
	m.AddNetEaseID("123")
	if !m.NetEaseIDConflict || m.NetEaseID != "" {
		t.Fatalf("later evidence cleared the conflict: %+v", m)
	}
	for _, tc := range []struct {
		id       string
		conflict bool
		status   string
		valid    bool
	}{
		{"123", false, "succeeded", true},
		{"", true, "succeeded", true},
		{"123", true, "succeeded", false},
		{"0123", false, "succeeded", false},
		{"123", false, "notAttempted", false},
		{"", true, "failed", false},
	} {
		_, err := library.EvidenceForObservation(library.Observation{NetEaseID: tc.id, NetEaseIDConflict: tc.conflict, ProbeStatus: tc.status})
		if (err == nil) != tc.valid {
			t.Fatalf("unexpected validation result for %+v: %v", tc, err)
		}
	}
}
