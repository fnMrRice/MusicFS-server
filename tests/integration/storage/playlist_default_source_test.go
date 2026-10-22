package storage_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
)

func playlistQualityObservation(name, version string, bitrate, size int64) library.Observation {
	o := evidenceObservation(name, version)
	o.BitrateBps, o.SizeBytes = &bitrate, size
	return o
}

func playlistSelected(t *testing.T, snapshot playlists.AggregateSnapshot, expected string) {
	t.Helper()
	if len(snapshot.Entries) != 1 || snapshot.Entries[0].Source.SourceTrackID == nil || *snapshot.Entries[0].Source.SourceTrackID != expected {
		t.Fatalf("unexpected selected playlist source: %+v", snapshot.Entries)
	}
}

func TestPlaylistDefaultVersionRespectsManualChoiceAndVisibleContributions(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	observations := []library.Observation{
		playlistQualityObservation("a", "v1", 128000, 9000),
		playlistQualityObservation("b", "v1", 256000, 8000),
		playlistQualityObservation("c", "v1", 256000, 10000),
		playlistQualityObservation("d", "v1", 320000, 15000),
	}
	tracks := publishEvidenceTracks(t, s, observations...)
	ids := []string{tracks[0].ID, tracks[1].ID, tracks[2].ID, tracks[3].ID}
	linked, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	revision := linked.Revision
	choose := func(source *string) {
		t.Helper()
		result, err := s.SetDefaultSource(ctx, "admin", matching.DefaultSourceRequest{ExpectedRevision: revision, RecordingID: *linked.RecordingID, SourceTrackID: source}, domain.NewID())
		if err != nil {
			t.Fatal(err)
		}
		revision = result.Revision
	}
	p := playlistCreate(t, s, "admin", "Versions", localBinding(ids[:3]...))
	first := playlistBuild(t, s, "admin", p)
	playlistSelected(t, first, ids[2])
	if len(first.Entries[0].Provenance) != 3 || *first.Entries[0].Provenance[0].Source.SourceTrackID != ids[0] {
		t.Fatal("quality selection changed contribution order")
	}
	choose(&ids[0])
	playlistSelected(t, playlistBuild(t, s, "admin", p), ids[0])
	choose(nil)
	playlistSelected(t, playlistBuild(t, s, "admin", p), ids[2])
	// 歌单外的手动版本不应扩大已有来源集合，单来源条目也保持用户指定。
	choose(&ids[3])
	playlistSelected(t, playlistBuild(t, s, "admin", p), ids[2])
	single := playlistCreate(t, s, "admin", "Explicit source", localBinding(ids[0]))
	playlistSelected(t, playlistBuild(t, s, "admin", single), ids[0])
	choose(&ids[0])
	binding := p.Bindings[0]
	_, err = s.HidePlaylistSource(ctx, "admin", p.ID, playlists.HideRequest{
		Scope: "entry", Source: binding.Entries[0].Source, BindingPath: []string{binding.ID}, EntryID: &binding.Entries[0].ID,
	}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	afterHide := playlistBuild(t, s, "admin", p)
	playlistSelected(t, afterHide, ids[2])
	if !afterHide.Entries[0].Provenance[0].Hidden {
		t.Fatal("default source revived a hidden contribution")
	}
	// 自动选择避开缺失来源，但明确选择该来源后仍保留手动意图。
	publishEvidenceTracks(t, s, observations[0], observations[1], observations[3])
	playlistSelected(t, playlistBuild(t, s, "admin", p), ids[1])
	choose(&ids[2])
	playlistSelected(t, playlistBuild(t, s, "admin", p), ids[2])
	old, err := s.PlaylistSnapshot(ctx, "admin", p.ID, first.Revision)
	if err != nil || !reflect.DeepEqual(old, first) {
		t.Fatal("default changes rewrote the historical snapshot", err)
	}
}

func TestPlaylistDefaultVersionRebuildUsesCurrentQualityWithoutMatchingChange(t *testing.T) {
	s := evidenceStore(t)
	ctx := context.Background()
	observations := []library.Observation{
		playlistQualityObservation("a", "v1", 128000, 9000),
		playlistQualityObservation("b", "v1", 256000, 8000),
	}
	tracks := publishEvidenceTracks(t, s, observations...)
	ids := []string{tracks[0].ID, tracks[1].ID}
	if _, err := s.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: ids}, domain.NewID()); err != nil {
		t.Fatal(err)
	}
	child := playlistCreate(t, s, "admin", "Child versions", localBinding(ids...))
	parent := playlistCreate(t, s, "admin", "Parent versions", playlists.PlaylistBinding{Kind: "playlist", TargetID: &child.ID})
	first := playlistBuild(t, s, "admin", parent)
	playlistSelected(t, first, ids[1])
	observations[0] = playlistQualityObservation("a", "v2", 320000, 9000)
	publishEvidenceTracks(t, s, observations...)
	second := playlistBuild(t, s, "admin", parent)
	playlistSelected(t, second, ids[0])
	if second.Revision <= first.Revision || second.MatchingRevision != first.MatchingRevision || second.Dependencies[0].Revision <= first.Dependencies[0].Revision {
		t.Fatal("quality update reused a stale parent or child snapshot")
	}
	observations[1] = playlistQualityObservation("b", "v2", 320000, 10000)
	publishEvidenceTracks(t, s, observations...)
	third := playlistBuild(t, s, "admin", parent)
	playlistSelected(t, third, ids[1])
	if third.Revision <= second.Revision {
		t.Fatal("file-size tiebreak did not publish a new snapshot")
	}
	if reused := playlistBuild(t, s, "admin", parent); reused.Revision != third.Revision {
		t.Fatal("unchanged selected sources failed to reuse their snapshot")
	}
	old, err := s.PlaylistSnapshot(ctx, "admin", parent.ID, first.Revision)
	if err != nil || !reflect.DeepEqual(old, first) {
		t.Fatal("quality update mutated the historical snapshot", err)
	}
}
