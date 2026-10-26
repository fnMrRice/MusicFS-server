package storage_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/plugins/pluginrpc"
)

func selectedTrackIDs(result library.TrackSelectionResult) []string {
	ids := make([]string, 0, len(result.Items))
	for _, track := range result.Items {
		ids = append(ids, track.ID)
	}
	return ids
}

func libraryTrackIDs(tracks []library.Track) []string {
	ids := make([]string, 0, len(tracks))
	for _, track := range tracks {
		ids = append(ids, track.ID)
	}
	return ids
}

func TestTrackSelectionBatchExclusionsAndExplicitScope(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	other := groupedPublish(t, s, "admin", "other-provider", evidenceObservation("other-version", "v1"))
	private := matchingPrivateTrack(t, s)
	page, err := s.ListTracks(t.Context(), "admin", "local", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	selection := library.TrackSelection{Batch: true, ProviderInstanceID: "local", LibraryRevision: page.LibraryRevision}
	got, err := s.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || got.Total != len(page.Items) || !reflect.DeepEqual(selectedTrackIDs(got), libraryTrackIDs(page.Items)) {
		t.Fatalf("batch selection differs from library rows: %v %v", got, err)
	}
	selection.Exclude = []string{tracks[1].ID}
	got, err = s.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || got.Total != 2 || len(got.Items) != 2 || got.Items[0].ID != tracks[0].ID || got.Items[1].ID != tracks[2].ID {
		t.Fatalf("exclusion failed: %v %v", got, err)
	}
	selection.Exclude = []string{tracks[0].ID, tracks[1].ID, tracks[2].ID}
	got, err = s.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || got.Total != 0 || got.Items == nil || len(got.Items) != 0 {
		t.Fatalf("empty batch selection is invalid: %v %v", got, err)
	}
	explicit := library.TrackSelection{SourceTrackIDs: []string{tracks[2].ID, tracks[0].ID}, ProviderInstanceID: "local"}
	got, err = s.ResolveTrackSelection(t.Context(), "admin", explicit)
	if err != nil || got.Total != 2 || len(got.Items) != 2 || got.Items[0].ID != tracks[0].ID || got.Items[1].ID != tracks[2].ID {
		t.Fatalf("explicit selection lost library order: %v %v", got, err)
	}
	for _, id := range []string{other.ID, private.ID, "unknown"} {
		explicit.SourceTrackIDs = []string{tracks[0].ID, id}
		_, err = s.ResolveTrackSelection(t.Context(), "admin", explicit)
		matchingError(t, err, "NOT_FOUND")
		selection.Exclude = []string{id}
		_, err = s.ResolveTrackSelection(t.Context(), "admin", selection)
		matchingError(t, err, "NOT_FOUND")
	}
	selection.ProviderInstanceID, selection.Exclude = "", nil
	got, err = s.ResolveTrackSelection(t.Context(), "bob", selection)
	if err != nil || got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != private.ID {
		t.Fatalf("batch selection crossed account boundary: %v %v", got, err)
	}
}

func TestTrackSelectionGroupedProviderAndRepresentativeExclusion(t *testing.T) {
	s, observations, tracks := matchingFixture(t, 2)
	other := groupedPublish(t, s, "admin", "other-provider", evidenceObservation("other-version", "v1"))
	if _, err := s.ManualLink(t.Context(), "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, other.ID}}, "selection-cross-source"); err != nil {
		t.Fatal(err)
	}
	// 本地成员缺失时，来源过滤仍应保留整个录音，并选取可用的另一来源为代表。
	publishEvidenceTracks(t, s, observations[1])
	page, err := s.ListGroupedTracks(t.Context(), "admin", "local", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	selection := library.TrackSelection{Batch: true, ProviderInstanceID: "local", GroupBy: "recording",
		LibraryRevision: page.LibraryRevision, MatchingRevision: page.MatchingRevision}
	got, err := s.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || got.Total != len(page.Items) || !reflect.DeepEqual(selectedTrackIDs(got), libraryTrackIDs(page.Items)) {
		t.Fatalf("grouped selection differs from list: %v %v", got, err)
	}
	selection.Exclude = []string{other.ID}
	got, err = s.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != tracks[1].ID {
		t.Fatalf("representative exclusion did not remove its entire row: %v %v", got, err)
	}
	selection.Batch, selection.Exclude, selection.SourceTrackIDs = false, nil, []string{other.ID}
	got, err = s.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != other.ID {
		t.Fatalf("explicit grouped selection lost its representative: %v %v", got, err)
	}
}

func TestTrackSelectionRejectsInvalidInputsAndChangedRevisions(t *testing.T) {
	s, observations, tracks := matchingFixture(t, 2)
	page, err := s.ListGroupedTracks(t.Context(), "admin", "", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	valid := library.TrackSelection{Batch: true, GroupBy: "recording", LibraryRevision: page.LibraryRevision, MatchingRevision: page.MatchingRevision}
	negative := int64(-1)
	for _, selection := range []library.TrackSelection{
		{}, {Batch: true}, {Batch: true, LibraryRevision: page.LibraryRevision, SourceTrackIDs: []string{tracks[0].ID}},
		{SourceTrackIDs: []string{tracks[0].ID}, Exclude: []string{tracks[1].ID}},
		{SourceTrackIDs: []string{tracks[0].ID, tracks[0].ID}}, {SourceTrackIDs: []string{" "}},
		{Batch: true, GroupBy: "recording", LibraryRevision: page.LibraryRevision},
		{Batch: true, GroupBy: "album", LibraryRevision: page.LibraryRevision},
		{Batch: true, LibraryRevision: "invalid"}, {Batch: true, LibraryRevision: page.LibraryRevision, MatchingRevision: &negative},
	} {
		_, err := s.ResolveTrackSelection(t.Context(), "admin", selection)
		matchingError(t, err, "INVALID_ARGUMENT")
	}
	if _, err = s.ManualLink(t.Context(), "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[1].ID}}, "selection-changed-revision"); err != nil {
		t.Fatal(err)
	}
	_, err = s.ResolveTrackSelection(t.Context(), "admin", valid)
	matchingError(t, err, "CONFLICT")
	valid.GroupBy, valid.MatchingRevision = "", nil
	publishEvidenceTracks(t, s, observations...)
	_, err = s.ResolveTrackSelection(t.Context(), "admin", valid)
	matchingError(t, err, "CONFLICT")
}

func TestTrackSelectionPreservesOrdinaryMusicVisibility(t *testing.T) {
	s := visibilityStore(t)
	hidden := visibilityObservation("hidden", "disc/hidden.wav", "Artist", "Album", "Rock", "wav")
	visible := visibilityObservation("visible", "visible.wav", "Artist", "Album", "Rock", "wav")
	tracks := visibilityScan(t, s, "admin", "local", hidden, visible)
	visibilityPackage(t, s, "admin", "local", "music", "cd_dump", hidden)
	for _, grouped := range []bool{false, true} {
		page, err := s.ListGroupedTracks(t.Context(), "admin", "local", "", 200)
		if err != nil {
			t.Fatal(err)
		}
		selection := library.TrackSelection{Batch: true, LibraryRevision: page.LibraryRevision}
		if grouped {
			selection.GroupBy, selection.MatchingRevision = "recording", page.MatchingRevision
		}
		got, err := s.ResolveTrackSelection(t.Context(), "admin", selection)
		if err != nil || got.Total != 1 || len(got.Items) != 1 || got.Items[0].ID != tracks[1].ID {
			t.Fatalf("work-only source leaked into selection: %v %v", got, err)
		}
		selection.Batch, selection.SourceTrackIDs = false, []string{tracks[0].ID}
		_, err = s.ResolveTrackSelection(t.Context(), "admin", selection)
		matchingError(t, err, "NOT_FOUND")
	}
}

func TestTrackSelectionCompactResultCreatesNoListSnapshot(t *testing.T) {
	s, _, tracks := matchingFixture(t, 3)
	page, err := s.ListGroupedTracks(t.Context(), "admin", "local", "", 200)
	if err != nil {
		t.Fatal(err)
	}
	count := func() int {
		var value int
		if err := s.DB.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM library_snapshots").Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := count()
	service := library.New(s, jobs.New(s), []library.Runtime{{ID: "local", Owner: "admin"}, {ID: "foreign", Owner: "bob"}}, nil)
	selection := library.TrackSelection{Batch: true, GroupBy: "recording", LibraryRevision: page.LibraryRevision, MatchingRevision: page.MatchingRevision}
	result, err := service.SelectTracks(t.Context(), "admin", selection)
	if err != nil || result.Total != len(tracks) || len(result.Items) != len(tracks) {
		t.Fatalf("compact selection failed: %v %v", result, err)
	}
	if count() != before {
		t.Fatal("selection created a retained list snapshot")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	for _, item := range value.Items {
		for _, field := range []string{"versions", "asset", "recordingId", "codec", "container", "path", "owner"} {
			if _, exists := item[field]; exists {
				t.Fatalf("compact reference exposed %s", field)
			}
		}
		for _, field := range []string{"sourceTrackId", "providerInstanceId", "displayName", "title", "artists", "durationMs", "availability", "assetId"} {
			if _, exists := item[field]; !exists {
				t.Fatalf("compact reference omitted %s", field)
			}
		}
	}
	selection.ProviderInstanceID = "foreign"
	_, err = service.SelectTracks(t.Context(), "admin", selection)
	matchingError(t, err, "NOT_FOUND")
}

func TestTrackSelectionPersistenceWorker(t *testing.T) {
	options := storageWorker(t)
	s, _, tracks := matchingFixture(t, 3)
	// 完整分组的代表行与版本都会包含专辑字段；合计超过 RPC 单值限额，紧凑引用仍很小。
	album := strings.Repeat("x", pluginrpc.MaxValueBytes/6+1024)
	if _, err := s.DB.ExecContext(t.Context(), "UPDATE tracks SET document=json_set(document,'$.album',?)", album); err != nil {
		t.Fatal(err)
	}
	remote := remoteFixture(t, s, options)
	page, err := remote.ListGroupedTracks(t.Context(), "admin", "", "", 1)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("selection RPC fixture is unavailable: %v %v", page, err)
	}
	selection := library.TrackSelection{Batch: true, GroupBy: "recording",
		LibraryRevision: page.LibraryRevision, MatchingRevision: page.MatchingRevision}
	items, err := remote.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || items.Total != 3 || !reflect.DeepEqual(selectedTrackIDs(items), libraryTrackIDs(tracks)) {
		t.Fatalf("selection RPC did not trim expanded metadata before transmission: %v %v", items, err)
	}
	selection.Exclude = []string{page.Items[0].ID}
	items, err = remote.ResolveTrackSelection(t.Context(), "admin", selection)
	if err != nil || items.Total != 2 || !reflect.DeepEqual(selectedTrackIDs(items), libraryTrackIDs(tracks[1:])) {
		t.Fatalf("selection RPC lost rows or scope: %v %v", items, err)
	}
	selection.LibraryRevision = "0"
	_, err = remote.ResolveTrackSelection(t.Context(), "admin", selection)
	matchingError(t, err, "CONFLICT")
}
