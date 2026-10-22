package workpackages_test

import (
	"errors"
	"path"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
)

func fixture(t *testing.T) (*sqlite.Store, works.Work, string) {
	t.Helper()
	filename := filepath.Join(t.TempDir(), "packages.sqlite")
	store, err := sqlite.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	work := createWork(t, store, "alice")
	return store, work, filename
}

func createWork(t *testing.T, store *sqlite.Store, owner string) works.Work {
	t.Helper()
	work, err := store.CreateWork(t.Context(), owner, works.CreateRequest{ContentType: "asmr", Title: "雨夜音声"}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func publication(work string) workpackages.Publication {
	root := "RJ123456"
	p := workpackages.Package{WorkID: work, ProviderID: "local", RootID: "music", RelativePath: root, Name: root,
		RootIdentity: "root-identity", ConfigRevision: "config-v1", Signature: "snapshot-v1", Nodes: []workpackages.Node{
			{Path: ".", Name: root, Kind: "directory", SourcePath: root},
			{Path: "WAV", Name: "WAV", Kind: "directory", SourcePath: root + "/WAV", Order: 1},
			{Path: "WAV/01.wav", Name: "01.wav", Kind: "audio", SourcePath: root + "/WAV/01.wav", Order: 2, MIME: "audio/wav", SizeBytes: 100, Version: "audio-v1", ExternalID: "track-one"},
			{Path: "資料", Name: "資料", Kind: "directory", SourcePath: root + "/資料", Order: 3},
			{Path: "資料/脚本.txt", Name: "脚本.txt", Kind: "attachment", SourcePath: root + "/資料/脚本.txt", Order: 4, MIME: "text/plain", SizeBytes: 50, Version: "text-v1", AttachmentKind: "text"},
			{Path: "cover.jpg", Name: "cover.jpg", Kind: "attachment", SourcePath: root + "/cover.jpg", Order: 5, MIME: "image/jpeg", SizeBytes: 200, Version: "image-v1", AttachmentKind: "image"},
			{Path: "空目录", Name: "空目录", Kind: "directory", SourcePath: root + "/空目录", Order: 6},
		}}
	input := workpackages.Publication{Package: p, ExpectedWorkRevision: 1}
	refreshObservations(&input)
	return input
}

func refreshObservations(input *workpackages.Publication) {
	input.Observations = []library.Observation{}
	for _, node := range input.Package.Nodes {
		if node.Kind != "audio" {
			continue
		}
		input.Observations = append(input.Observations, library.Observation{Observation: pluginproto.Observation{
			ExternalID: node.ExternalID, RootID: input.Package.RootID, RelativePath: node.SourcePath, DisplayName: path.Base(node.SourcePath),
			ObservedVersion: node.Version, SizeBytes: node.SizeBytes, MIME: node.MIME}, Artists: []string{}})
	}
}

func publish(t *testing.T, store *sqlite.Store, owner string, input workpackages.Publication) workpackages.Package {
	t.Helper()
	p, err := store.PublishWorkPackage(t.Context(), owner, input, domain.NewID(), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func requireCode(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func nodeAt(t *testing.T, p workpackages.Package, name string) workpackages.Node {
	t.Helper()
	for _, node := range p.Nodes {
		if node.Path == name {
			return node
		}
	}
	t.Fatalf("package node is missing: %s", name)
	return workpackages.Node{}
}
