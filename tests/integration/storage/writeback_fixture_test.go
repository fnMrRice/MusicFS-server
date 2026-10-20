package storage_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/writeback"
	"github.com/fnMrRice/MusicFS-server/src/writebackhost"
)

type writebackFixture struct {
	s                *sqlite.Store
	track            library.Track
	observation      library.Observation
	request          writebackhost.Request
	target           writeback.Target
	english, unknown mediastore.ContentObject
}

func newWritebackFixture(t *testing.T) writebackFixture {
	t.Helper()
	s, items, tracks := matchingFixture(t, 1)
	ctx := context.Background()
	track, err := s.FindTrack(ctx, "admin", tracks[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	english := contentPublish(t, s, "admin", contentInput(track, "eng", "Selected English lyrics"))
	unknown := contentPublish(t, s, "admin", contentInput(track, "und", "Selected unknown-language lyrics"))
	if _, err = s.SelectContent(ctx, "admin", mediastore.SelectRequest{Target: english.Metadata.Target, ObjectID: english.ID, ExpectedRevision: 0}, "before-writeback-selection"); err != nil {
		t.Fatal(err)
	}
	contentSelect(t, s, unknown, 1, true)
	target := writeback.Target{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Version, InstanceID: track.ProviderID, RootID: track.RootID, RelativePath: track.Path, FileIdentity: "original-file-identity"}
	r := writebackhost.Request{SourceTrackID: track.ID, AssetID: track.AssetID, AssetVersion: track.Version, ExpectedRevision: 2, Fields: []writebackhost.Field{{Name: "lyrics", Language: "en"}}}
	return writebackFixture{s, track, items[0], r, target, english, unknown}
}

func (f writebackFixture) queue(t *testing.T) writebackhost.Job {
	t.Helper()
	j, replay, err := f.s.QueueWriteback(context.Background(), "admin", f.request, f.target, domain.NewID())
	if err != nil || replay {
		t.Fatal(j, err)
	}
	return j
}

func (f writebackFixture) result(j writebackhost.Job, changed bool) (writeback.Result, library.Observation) {
	r := writeback.Result{JobID: j.ID, SourceTrackID: f.track.ID, Old: writeback.FileState{Identity: f.target.FileIdentity, Version: f.target.AssetVersion, SHA512: strings.Repeat("a", 128), Size: f.track.Size}, AudioSHA512: strings.Repeat("c", 128), Changed: changed}
	r.New = r.Old
	o := f.observation
	if changed {
		r.New = writeback.FileState{Identity: "replacement-file-identity", Version: "writeback-v2", SHA512: strings.Repeat("b", 128), Size: f.track.Size + 64}
	}
	o.ObservedVersion = r.New.Version
	o.SizeBytes = r.New.Size
	return r, o
}
