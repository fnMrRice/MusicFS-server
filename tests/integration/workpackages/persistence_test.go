package workpackages_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
)

func TestPackageTreeTracksAttachmentsPersistAndReplayAfterRestart(t *testing.T) {
	store, work, filename := fixture(t)
	input := publication(work.ID)
	p, err := store.PublishWorkPackage(t.Context(), "alice", input, "preview-one", "import-once")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID == "" || p.Revision != 1 || p.ImportedAt.IsZero() || len(p.Nodes) != 7 {
		t.Fatal("invalid package identity or tree")
	}
	duplicate, err := store.PublishWorkPackage(t.Context(), "alice", input, "preview-one", "import-once")
	if err != nil || !reflect.DeepEqual(duplicate, p) {
		t.Fatal("repeated confirmation changed the package", err)
	}
	root, audio := nodeAt(t, p, "."), nodeAt(t, p, "WAV/01.wav")
	if root.ParentID != "" || nodeAt(t, p, "WAV").ParentID != root.ID || audio.ParentID != nodeAt(t, p, "WAV").ID {
		t.Fatal("incorrect tree parents")
	}
	if audio.SourceTrackID == "" || audio.AssetID == "" {
		t.Fatal("audio has no source binding")
	}
	track, err := store.FindTrack(t.Context(), "alice", audio.SourceTrackID)
	if err != nil || track.AssetID != audio.AssetID || track.ProviderID != p.ProviderID || track.Path != audio.SourcePath || track.Version != audio.Version {
		t.Fatal("invalid source binding", err)
	}
	for _, name := range []string{"資料/脚本.txt", "cover.jpg", "空目录"} {
		node := nodeAt(t, p, name)
		if node.SourceTrackID != "" || node.AssetID != "" {
			t.Fatal("non-audio node became a source track")
		}
	}
	public, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"rootIdentity", "configRevision", "signature", "sourcePath", "externalId", "audio-v1", "text-v1"} {
		if strings.Contains(string(public), private) {
			t.Fatalf("public package leaks %s", private)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(t.Context(), filename)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { reopened.Close() })
	read, err := reopened.WorkPackage(t.Context(), "alice", work.ID, p.ID)
	if err != nil || !reflect.DeepEqual(read, p) {
		t.Fatal("tree or private binding changed after reopening", err)
	}
	replay, found, err := reopened.ReplayWorkPackage(t.Context(), "alice", work.ID, "preview-one", "import-once")
	if err != nil || !found || !reflect.DeepEqual(replay, p) {
		t.Fatal("import receipt did not survive restart", err)
	}
	_, _, err = reopened.ReplayWorkPackage(t.Context(), "alice", work.ID, "other-preview", "import-once")
	requireCode(t, err, "CONFLICT")
	_, found, err = reopened.ReplayWorkPackage(t.Context(), "bob", work.ID, "preview-one", "import-once")
	if err != nil || found {
		t.Fatal("receipt crossed owners", err)
	}
	_, err = reopened.WorkPackage(t.Context(), "bob", work.ID, p.ID)
	requireCode(t, err, "NOT_FOUND")
	_, err = reopened.WorkPackages(t.Context(), "bob", work.ID)
	requireCode(t, err, "NOT_FOUND")
	listed, err := reopened.WorkPackages(t.Context(), "alice", work.ID)
	if err != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], p) {
		t.Fatal("package list changed", err)
	}
	currentWork, err := reopened.Work(t.Context(), "alice", work.ID)
	if err != nil || !reflect.DeepEqual(currentWork, work) {
		t.Fatal("import changed work metadata revision", err)
	}
}

func TestPackageLocationsTracksAndReceiptsRemainOwnerScoped(t *testing.T) {
	store, work, _ := fixture(t)
	other := createWork(t, store, "bob")
	alice, err := store.PublishWorkPackage(t.Context(), "alice", publication(work.ID), "shared-preview", "shared-import")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := store.PublishWorkPackage(t.Context(), "bob", publication(other.ID), "shared-preview", "shared-import")
	if err != nil {
		t.Fatal("another owner could not import the same location", err)
	}
	audioA, audioB := nodeAt(t, alice, "WAV/01.wav"), nodeAt(t, bob, "WAV/01.wav")
	if alice.ID == bob.ID || audioA.SourceTrackID == audioB.SourceTrackID || audioA.AssetID == audioB.AssetID {
		t.Fatal("package or source identity crossed owners")
	}
	_, err = store.FindTrack(t.Context(), "bob", audioA.SourceTrackID)
	requireCode(t, err, "NOT_FOUND")
	input := publication(work.ID)
	input.Package.Owner = "bob"
	_, err = store.PublishWorkPackage(t.Context(), "alice", input, "foreign-preview", "foreign-import")
	requireCode(t, err, "ACCESS_DENIED")
}

func TestPackageRefreshPreservesNodesAndTracksWhileReplacingAssetVersions(t *testing.T) {
	store, work, _ := fixture(t)
	input := publication(work.ID)
	first := publish(t, store, "alice", input)
	audio := nodeAt(t, first, "WAV/01.wav")
	input.Package = first
	input.ExpectedPackageRevision = first.Revision
	input.Package.Nodes = append([]workpackages.Node{}, first.Nodes...)
	for i := range input.Package.Nodes {
		node := &input.Package.Nodes[i]
		if node.Kind == "audio" {
			node.Path, node.Name, node.SourcePath = "WAV/02.wav", "02.wav", "RJ123456/WAV/02.wav"
			node.Version = "audio-v2"
		}
	}
	refreshObservations(&input)
	next := publish(t, store, "alice", input)
	renamed := nodeAt(t, next, "WAV/02.wav")
	if next.ID != first.ID || next.Revision != 2 || renamed.ID != audio.ID || renamed.SourceTrackID != audio.SourceTrackID || renamed.AssetID == audio.AssetID {
		t.Fatal("refresh lost stable identity or asset replacement")
	}
	if nodeAt(t, next, "資料/脚本.txt").ID != nodeAt(t, first, "資料/脚本.txt").ID || nodeAt(t, next, ".").ID != nodeAt(t, first, ".").ID {
		t.Fatal("unchanged nodes lost identity")
	}
	previousAsset, err := store.Asset(t.Context(), "alice", audio.AssetID)
	if err != nil || previousAsset.Current {
		t.Fatal("old asset remained current", err)
	}
	newAsset, err := store.Asset(t.Context(), "alice", renamed.AssetID)
	if err != nil || !newAsset.Current {
		t.Fatal("new asset is not current", err)
	}
	_, err = store.PublishWorkPackage(t.Context(), "alice", input, "stale-preview", "stale-import")
	requireCode(t, err, "CONFLICT")
	other := createWork(t, store, "alice")
	foreign := publication(other.ID)
	_, err = store.PublishWorkPackage(t.Context(), "alice", foreign, "other-preview", "other-import")
	requireCode(t, err, "CONFLICT")
	input.Package, input.ExpectedPackageRevision = next, next.Revision
	input.Package.Nodes = []workpackages.Node{nodeAt(t, next, ".")}
	input.Observations = nil
	last := publish(t, store, "alice", input)
	if len(last.Nodes) != 1 {
		t.Fatal("tree replacement kept obsolete nodes")
	}
	retained, err := store.FindTrack(t.Context(), "alice", audio.SourceTrackID)
	if err != nil || retained.Availability != "available" {
		t.Fatal("package refresh inferred missing source", err)
	}
}
