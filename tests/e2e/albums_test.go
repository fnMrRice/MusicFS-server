package e2e_test

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/albums"
	"github.com/fnMrRice/MusicFS-server/src/config"
	"github.com/fnMrRice/MusicFS-server/src/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/mediastore"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

func TestAlbumsActualServerSnapshotDocumentsThumbnailAndRestart(t *testing.T) {
	provider, scraper := build(t, "musicfs-plugin-local"), build(t, "musicfs-plugin-scraper-local")
	wave, err := os.ReadFile(filepath.Join("..", "testdata", "audio", "markers.wav"))
	if err != nil {
		t.Fatal(err)
	}
	music := t.TempDir()
	samples := map[string][]byte{"a.wav": wave, "b.wav": wave, "a.lrc": []byte("[00:01.00]Synthetic album A"), "b.lrc": []byte("[00:01.00]Synthetic album B"), "a.png": testkit.ScraperPNG(), "b.png": testkit.ScraperPNG()}
	for name, raw := range samples {
		if err := os.WriteFile(filepath.Join(music, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	cfg, base := yamlServiceConfig(t)
	cfg.Plugins = []config.Plugin{
		{PluginID: "musicfs.plugin.source.localfiles", Executable: provider, Enabled: true, Instances: []config.Instance{{InstanceID: "b1548e96-3081-5c9c-8376-bf290e369d51", Enabled: true, Config: config.InstanceConfig{MusicRoots: []config.MusicRoot{{ID: "local", Name: "Album fixture", Path: music}}}}}},
		{PluginID: "musicfs.scraper.local", Executable: scraper, Enabled: true, Instances: []config.Instance{{InstanceID: "d248bf34-1780-59d3-a557-9cf5d889e977", Enabled: true}}},
	}
	var album albums.SourceAlbum
	var release albums.Release
	var job enrichment.Job
	var art enrichment.PreviewCandidate
	var thumbnail mediastore.DerivedObject
	var documents []mediastore.DerivedObject
	var ordered []string
	complete := false
	t.Run("album-snapshot-and-derived-content", func(t *testing.T) {
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		tracks := scanNCM(t, c)
		ordered = []string{tracks["b.wav"].ID, tracks["a.wav"].ID}
		album = albumRequest[albums.SourceAlbum](t, c, "POST", "/source-albums", map[string]any{"providerInstanceId": "b1548e96-3081-5c9c-8376-bf290e369d51", "name": "Synthetic album", "sourceTrackIds": ordered}, "album-create-key", 201, "SourceAlbum")
		if album.Revision != 1 || !reflect.DeepEqual(album.SourceTrackIDs, ordered) {
			t.Fatal("album did not preserve explicit membership order")
		}
		release = albumRequest[albums.Release](t, c, "POST", "/releases", map[string]any{"name": "Synthetic release", "sourceAlbumIds": []string{album.ID}}, "release-create-key", 201, "Release")
		input := map[string]any{"sourceAlbum": map[string]any{"sourceAlbumId": album.ID, "expectedRevision": album.Revision}, "fields": []string{"lyrics", "artwork"}, "instanceIds": []string{"d248bf34-1780-59d3-a557-9cf5d889e977"}}
		job = metadataJob(t, c, input, "album-metadata-key")
		if job.State != "completed" || len(job.Targets) != 2 || job.Request.SourceAlbum == nil || job.Request.SourceAlbum.ExpectedRevision != 1 || len(job.Request.SourceTrackIDs) != 0 {
			t.Fatalf("album snapshot not retained: %+v", job)
		}
		for _, target := range job.Targets {
			if target.State != "completed" || target.Target.Revision != 0 || target.Target.AssetID == "" || target.Target.AssetVersion == "" {
				t.Fatalf("invalid fixed target: %+v", target)
			}
			if selection := metadataSelection(t, c, target.Target.SourceTrackID); selection.Revision != 0 || len(selection.Bindings) != 0 {
				t.Fatal("album search changed a source selection")
			}
			for _, candidate := range target.Candidates {
				if candidate.Field == "artwork" && candidate.Target.SourceTrackID == ordered[1] && candidate.PreviewObjectID != nil {
					art = candidate
				}
			}
		}
		if art.PreviewObjectID == nil {
			t.Fatal("album task produced no artwork preview")
		}
		album = albumRequest[albums.SourceAlbum](t, c, "PUT", "/source-albums/"+album.ID, map[string]any{"expectedRevision": 1, "name": "Edited album", "sourceTrackIds": []string{ordered[1]}}, "album-edit-key", 200, "SourceAlbum")
		release = albumRequest[albums.Release](t, c, "GET", "/releases/"+release.ID, nil, "", 200, "Release")
		if album.Revision != 2 || release.Revision != 2 {
			t.Fatal("member revision did not propagate to release")
		}
		if replay := metadataJob(t, c, input, "album-metadata-key"); replay.JobID != job.JobID || !reflect.DeepEqual(replay.Targets, job.Targets) {
			t.Fatal("album edit reinterpreted an existing metadata request")
		}
		code, raw, _ := c.request(t, "POST", "/api/v1/metadata-jobs", input, "album-stale-key")
		if code != 409 {
			t.Fatalf("stale album revision: %d %s", code, raw)
		}
		for _, path := range []string{"/source-albums/" + album.ID, "/releases/" + release.ID} {
			cover := albumRequest[albums.CoverSelection](t, c, "PUT", path+"/cover-selection", map[string]any{"objectId": *art.PreviewObjectID, "expectedRevision": 0, "expectedTargetRevision": 2}, "album-cover-"+path[len(path)-8:], 200, "AlbumCoverSelection")
			if cover.Revision != 1 || !cover.ManualLocked || cover.Binding == nil || cover.Binding.ObjectID != *art.PreviewObjectID {
				t.Fatal("independent album/release cover was not locked")
			}
		}
		if selection := metadataSelection(t, c, ordered[1]); selection.Revision != 0 {
			t.Fatal("album cover selection modified source media selection")
		}
		documents = checkAlbumDocuments(t, c, job, music)
		thumbnail = albumRequest[mediastore.DerivedObject](t, c, "POST", "/media/"+*art.PreviewObjectID+"/thumbnail", struct{}{}, "", 200, "DerivedObject")
		if replay := albumRequest[mediastore.DerivedObject](t, c, "POST", "/media/"+*art.PreviewObjectID+"/thumbnail", struct{}{}, "", 200, "DerivedObject"); replay.ID != thumbnail.ID {
			t.Fatal("thumbnail retry created a duplicate")
		}
		checkAlbumThumbnail(t, c, thumbnail, samples["a.png"])
		album = albumRequest[albums.SourceAlbum](t, c, "PUT", "/source-albums/"+album.ID, map[string]any{"expectedRevision": 2, "name": "Empty historical album", "sourceTrackIds": []string{}}, "album-empty-key", 200, "SourceAlbum")
		complete = true
	})
	t.Run("disabled-restart-retains-history-and-playback", func(t *testing.T) {
		if !complete {
			t.Skip("initial phase failed")
		}
		cfg.Plugins[1].Enabled = false
		startNCMServer(t, cfg, base)
		c := login(t, base, cfg.AdminToken)
		for _, path := range []string{"/source-albums/" + album.ID, "/releases/" + release.ID} {
			cover := albumRequest[albums.CoverSelection](t, c, "GET", path+"/cover-selection", nil, "", 200, "AlbumCoverSelection")
			if cover.Revision != 1 || !cover.ManualLocked || cover.Binding == nil || cover.Binding.ObjectID != *art.PreviewObjectID {
				t.Fatal("membership edit or restart lost historical cover lock")
			}
		}
		restored := albumRequest[enrichment.Job](t, c, "GET", "/metadata-jobs/"+job.JobID, nil, "", 200, "MetadataJob")
		if !reflect.DeepEqual(restored.Targets, job.Targets) || restored.Request.SourceAlbum.ExpectedRevision != 1 {
			t.Fatal("restart changed metadata target snapshot")
		}
		if after := checkAlbumDocuments(t, c, restored, music); !reflect.DeepEqual(after, documents) {
			t.Fatal("disabled restart changed scrape documents")
		}
		checkAlbumThumbnail(t, c, thumbnail, samples["a.png"])
		code, raw, _ := c.request(t, "POST", "/api/v1/playback/sessions", playback.CreateRequest{SourceTrackID: ordered[1], Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}}, "album-playback-key")
		var description playback.Description
		if code != 201 || json.Unmarshal(raw, &description) != nil {
			t.Fatalf("album playback regression: %d %s", code, raw)
		}
		verifyRanges(t, c, description, wave)
		if code, _, _ := c.request(t, "DELETE", "/api/v1/playback/sessions/"+description.SessionID, nil, ""); code != 204 {
			t.Fatal("stop failed")
		}
		for name, original := range samples {
			actual, err := os.ReadFile(filepath.Join(music, name))
			if err != nil || sha256.Sum256(actual) != sha256.Sum256(original) {
				t.Fatal("album workflow changed source or sidecar bytes")
			}
		}
	})
}
