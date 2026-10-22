package workpackages_test

import (
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

func TestPackageImportRejectsMalformedOrIncompleteSnapshots(t *testing.T) {
	store, work, _ := fixture(t)
	for _, test := range []struct {
		name   string
		mutate func(*workpackages.Publication)
	}{
		{"missing-root", func(p *workpackages.Publication) { p.Package.Nodes = p.Package.Nodes[1:] }},
		{"empty-tree", func(p *workpackages.Publication) { p.Package.Nodes = nil }},
		{"parent-escape", func(p *workpackages.Publication) { p.Package.RelativePath = "../escape" }},
		{"absolute-path", func(p *workpackages.Publication) { p.Package.RelativePath = "C:/escape" }},
		{"mixed-separators", func(p *workpackages.Publication) { p.Package.Nodes[2].Path = "WAV\\01.wav" }},
		{"duplicate-path", func(p *workpackages.Publication) { p.Package.Nodes = append(p.Package.Nodes, p.Package.Nodes[2]) }},
		{"missing-parent", func(p *workpackages.Publication) {
			p.Package.Nodes[1].Path = "other"
			p.Package.Nodes[1].Name = "other"
			p.Package.Nodes[1].SourcePath = "RJ123456/other"
		}},
		{"mismatched-name", func(p *workpackages.Publication) { p.Package.Nodes[2].Name = "another.wav" }},
		{"wrong-source-path", func(p *workpackages.Publication) { p.Package.Nodes[2].SourcePath = "outside.wav" }},
		{"missing-version", func(p *workpackages.Publication) { p.Package.Nodes[2].Version = "" }},
		{"missing-root-binding", func(p *workpackages.Publication) { p.Package.RootIdentity = "" }},
		{"invalid-attachment", func(p *workpackages.Publication) { p.Package.Nodes[4].AttachmentKind = "audio" }},
		{"directory-size", func(p *workpackages.Publication) { p.Package.Nodes[1].SizeBytes = 1 }},
		{"directory-version", func(p *workpackages.Publication) { p.Package.Nodes[1].Version = strings.Repeat("x", 513) }},
		{"no-observation", func(p *workpackages.Publication) { p.Observations = nil }},
		{"duplicate-observation", func(p *workpackages.Publication) { p.Observations = append(p.Observations, p.Observations[0]) }},
		{"observation-version", func(p *workpackages.Publication) { p.Observations[0].ObservedVersion = "wrong-version" }},
		{"observation-root", func(p *workpackages.Publication) { p.Observations[0].RootID = "another-root" }},
		{"node-limit", func(p *workpackages.Publication) {
			p.Package.Nodes = make([]workpackages.Node, workpackages.MaxNodes+1)
		}},
		{"path-limit", func(p *workpackages.Publication) {
			p.Package.RelativePath = strings.Repeat("x", workpackages.MaxPathBytes+1)
		}},
		{"joined-path-limit", func(p *workpackages.Publication) {
			p.Package.RelativePath = strings.Repeat("x", workpackages.MaxPathBytes)
			for i := range p.Package.Nodes {
				p.Package.Nodes[i].SourcePath = p.Package.RelativePath
				if p.Package.Nodes[i].Path != "." {
					p.Package.Nodes[i].SourcePath += "/" + p.Package.Nodes[i].Path
				}
			}
			refreshObservations(p)
		}},
		{"depth-limit", func(p *workpackages.Publication) {
			p.Package.Nodes[2].Path = strings.Repeat("dir/", workpackages.MaxDepth) + "01.wav"
			p.Package.Nodes[2].SourcePath = p.Package.RelativePath + "/" + p.Package.Nodes[2].Path
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := publication(work.ID)
			test.mutate(&input)
			_, err := store.PublishWorkPackage(t.Context(), "alice", input, domain.NewID(), domain.NewID())
			requireCode(t, err, "INVALID_ARGUMENT")
		})
	}
	items, err := store.WorkPackages(t.Context(), "alice", work.ID)
	if err != nil || len(items) != 0 {
		t.Fatal("invalid publication left a package", err)
	}
	var tracks int
	if err = store.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM tracks").Scan(&tracks); err != nil || tracks != 0 {
		t.Fatal("invalid publication left tracks", err)
	}
}
