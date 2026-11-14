package storage_test

import (
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/personal"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func visibilityStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "visibility.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func visibilityObservation(id, filename, artist, album, genre, format string) library.Observation {
	year, rate := int64(2024), int64(48000)
	return library.Observation{Observation: pluginproto.Observation{ExternalID: id, RootID: "music", RelativePath: filename,
		ObservedVersion: "v1", DisplayName: path.Base(filename), MIME: "audio/wav", SizeBytes: 100},
		Artists: []string{artist}, Album: &album, Genre: &genre, Container: &format, Year: &year, SampleRateHz: &rate}
}

func visibilityScan(t *testing.T, s *sqlite.Store, owner, provider string, observations ...library.Observation) []library.Track {
	t.Helper()
	roots := []string{}
	for _, o := range observations {
		if !slices.Contains(roots, o.RootID) {
			roots = append(roots, o.RootID)
		}
	}
	if _, _, err := s.Enqueue(t.Context(), owner, provider, testkit.ScanPlan(roots...), domain.NewID()); err != nil {
		t.Fatal(err)
	}
	job, err := s.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Publish(t.Context(), job, testkit.ScanPublication(job, observations)); err != nil {
		t.Fatal(err)
	}
	if err = s.Finish(t.Context(), job, len(observations), nil); err != nil {
		t.Fatal(err)
	}
	result := []library.Track{}
	for _, o := range observations {
		var id string
		if err = s.DB.QueryRowContext(t.Context(), "SELECT id FROM tracks WHERE owner=? AND provider=? AND external_id=?", owner, provider, o.ExternalID).Scan(&id); err != nil {
			t.Fatal(err)
		}
		track, err := s.FindTrack(t.Context(), owner, id)
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, track)
	}
	return result
}

func visibilityPackage(t *testing.T, s *sqlite.Store, owner, provider, contentType, kind string, o library.Observation) (workpackages.Package, workpackages.Publication) {
	t.Helper()
	w, err := s.CreateWork(t.Context(), owner, works.CreateRequest{ContentType: contentType, Title: contentType}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	directory := path.Dir(o.RelativePath)
	input := workpackages.Publication{ExpectedWorkRevision: w.Revision, Observations: []library.Observation{o}, Package: workpackages.Package{
		WorkID: w.ID, Kind: kind, ProviderID: provider, RootID: o.RootID, RelativePath: directory, Name: path.Base(directory),
		RootIdentity: "root", ConfigRevision: "config", Signature: "signature", Nodes: []workpackages.Node{
			{Path: ".", Name: path.Base(directory), Kind: "directory", SourcePath: directory},
			{Path: path.Base(o.RelativePath), Name: path.Base(o.RelativePath), Kind: "audio", SourcePath: o.RelativePath,
				Order: 1, MIME: o.MIME, SizeBytes: o.SizeBytes, Version: o.ObservedVersion, ExternalID: o.ExternalID},
		}}}
	p, err := s.PublishWorkPackage(t.Context(), owner, input, domain.NewID(), domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	return p, input
}

func visibilityIDs(t *testing.T, s *sqlite.Store, owner, provider string) []string {
	t.Helper()
	result := []string{}
	cursor := ""
	for {
		page, err := s.ListTracks(t.Context(), owner, provider, cursor, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, track := range page.Items {
			result = append(result, track.ID)
		}
		if page.NextCursor == nil {
			break
		}
		cursor = *page.NextCursor
	}
	slices.Sort(result)
	return result
}

func TestOrdinaryMusicPackageIdentityLocationAndOwnerIsolation(t *testing.T) {
	s := visibilityStore(t)
	o := visibilityObservation("scan", "shared/disc/song.wav", "Artist", "Album", "Rock", "wav")
	duplicate := o
	duplicate.ExternalID = "package"
	otherRoot := o
	otherRoot.ExternalID, otherRoot.RootID = "other-root", "other"
	outside := o
	outside.ExternalID, outside.RelativePath = "outside", "shared/disc/untagged.wav"
	tracks := visibilityScan(t, s, "admin", "local", o, duplicate, otherRoot, outside)
	foreign := visibilityScan(t, s, "bob", "local", o)
	otherProvider := visibilityScan(t, s, "admin", "other", o)
	// 先绑定同一录音，普通音乐分组必须剔除隐藏成员而保留录音身份。
	_, err := s.ManualLink(t.Context(), "admin", matching.LinkRequest{SourceTrackIDs: []string{tracks[0].ID, tracks[3].ID}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	visibilityPackage(t, s, "admin", "local", "cd_album", "cd_dump", duplicate)
	want := []string{tracks[2].ID, tracks[3].ID}
	slices.Sort(want)
	if got := visibilityIDs(t, s, "admin", "local"); !reflect.DeepEqual(got, want) {
		t.Fatalf("location filter = %v, want %v", got, want)
	}
	for _, scope := range []struct{ owner, provider, id string }{{"bob", "local", foreign[0].ID}, {"admin", "other", otherProvider[0].ID}} {
		if got := visibilityIDs(t, s, scope.owner, scope.provider); !reflect.DeepEqual(got, []string{scope.id}) {
			t.Fatalf("unrelated scope was hidden: %+v, %v", scope, got)
		}
	}
	grouped, err := s.ListGroupedTracks(t.Context(), "admin", "local", "", 200)
	if err != nil || len(grouped.Items) != 2 {
		t.Fatal(grouped, err)
	}
	found := false
	for _, track := range grouped.Items {
		if len(track.Versions) != 1 || !slices.Contains(want, track.ID) {
			t.Fatal("hidden recording member remained", track)
		}
		if track.ID == tracks[3].ID {
			found = track.RecordingID != nil
		}
	}
	if !found {
		t.Fatal("visible recording identity was lost")
	}
	// 已扫描来源随后改名，仍应由包节点的稳定来源 ID 排除。
	moved := duplicate
	moved.RelativePath = "moved/song.wav"
	later := o
	later.ExternalID = "after-import"
	visibilityScan(t, s, "admin", "local", o, moved, later, otherRoot, outside)
	if got := visibilityIDs(t, s, "admin", "local"); !reflect.DeepEqual(got, want) {
		t.Fatalf("source identity filter = %v, want %v", got, want)
	}
}

func TestCDAlbumTrackListSettingPreservesSourcesAndPersonalData(t *testing.T) {
	s := visibilityStore(t)
	o := visibilityObservation("song", "album/song.wav", "Artist", "Album", "Rock", "wav")
	track := visibilityScan(t, s, "admin", "local", o)[0]
	p, _ := visibilityPackage(t, s, "admin", "local", "cd_album", "directory", o)
	work, err := s.Work(t.Context(), "admin", p.WorkID)
	if err != nil || work.ShowInTrackList || len(visibilityIDs(t, s, "admin", "local")) != 0 {
		t.Fatal("CD album audio should be hidden by default", work, err)
	}
	if _, err := s.PutFavorite(t.Context(), "admin", track.ID); err != nil {
		t.Fatal(err)
	}
	list, err := s.CreatePlaylist(t.Context(), "admin", playlists.CreatePlaylistRequest{Name: "Keep", Bindings: []playlists.PlaylistBinding{
		{Kind: "local", Entries: []playlists.LocalEntry{{Source: playlists.SourceRef{SourceTrackID: &track.ID}}}},
	}}, domain.NewID())
	if err != nil {
		t.Fatal(err)
	}
	for _, show := range []bool{true, false} {
		work, err = s.UpdateWork(t.Context(), "admin", p.WorkID, works.UpdateRequest{
			ExpectedRevision: work.Revision, Title: work.Title, ShowInTrackList: show,
		}, domain.NewID())
		if err != nil || work.ShowInTrackList != show {
			t.Fatal("CD album setting was not saved", work, err)
		}
		want := []string{}
		if show {
			want = append(want, track.ID)
		}
		if got := visibilityIDs(t, s, "admin", "local"); !slices.Equal(got, want) {
			t.Fatalf("showInTrackList=%t: got %v, want %v", show, got, want)
		}
		current, err := s.FindTrack(t.Context(), "admin", track.ID)
		if err != nil || current.AssetID != track.AssetID || current.Availability != "available" {
			t.Fatal("source was changed", current, err)
		}
		favorites, err := s.PersonalCollection(t.Context(), "admin", personal.Favorites, "", 100)
		if err != nil || len(favorites.Items) != 1 || favorites.Items[0].Track.ID != track.ID {
			t.Fatal("favorite was lost", favorites, err)
		}
		snapshot, err := s.RebuildPlaylist(t.Context(), "admin", list.ID, playlists.RebuildRequest{ExpectedRuleRevision: list.RuleRevision}, domain.NewID())
		if err != nil || len(snapshot.Entries) != 1 || snapshot.Entries[0].Source.SourceTrackID == nil || *snapshot.Entries[0].Source.SourceTrackID != track.ID {
			t.Fatal("playlist source was lost", snapshot, err)
		}
		stored, err := s.WorkPackage(t.Context(), "admin", p.WorkID, p.ID)
		if err != nil || stored.Nodes[1].SourceTrackID != track.ID || stored.Kind != p.Kind {
			t.Fatal("package source was lost", stored, err)
		}
	}
}
