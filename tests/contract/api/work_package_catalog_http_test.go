package api_test

import (
	"bytes"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/albums"
	"github.com/fnMrRice/MusicFS-server/src/library/catalog"
	"github.com/fnMrRice/MusicFS-server/src/library/workpackages"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/playback"
)

func TestWorkPackagesHTTPSeparateLibrariesAndToggleCDAlbumTracks(t *testing.T) {
	f := newPackageCatalogHTTP(t)
	// 先扫描，再把同位置音频关联到作品，覆盖既有曲库条目的隐藏规则。
	f.scan(t, "catalog-initial-scan")
	var scanned library.Page
	f.call(t, "GET", "/source-tracks", nil, "", 200, "SourceTracks", &scanned)
	if len(scanned.Items) != len(f.waves) {
		t.Fatal("initial scan did not expose all tracks", len(scanned.Items))
	}
	tracks := map[string]library.Track{}
	albumIDs := map[string]string{}
	for _, track := range scanned.Items {
		if track.Title == nil {
			t.Fatal("fixture metadata was not probed")
		}
		name := *track.Title
		tracks[name] = track
		var album albums.SourceAlbum
		f.call(t, "POST", "/source-albums", albums.CreateSourceAlbumRequest{ProviderInstanceID: packageProvider, Name: name + "-album", SourceTrackIDs: []string{track.ID}}, "album-"+name, 201, "SourceAlbum", &album)
		albumIDs[name] = album.ID
	}
	var before catalog.Page
	f.call(t, "GET", "/library/search?type=albums", nil, "", 200, "CatalogPage", &before)
	if before.Total != len(f.waves) {
		t.Fatal("initial album fixture is incomplete", before.Total)
	}
	packages := []workpackages.Package{
		f.importCopy(t, "asmr-only", "asmr", "directory"),
		f.importCopy(t, "cd-only", "cd_album", "cd_dump"),
		f.importCopy(t, "music-only", "music", "directory"),
		f.importCopy(t, "radio-only", "radio", "directory"),
	}
	musicIDs := map[string]bool{tracks["music-only"].ID: true}
	for _, node := range packages[2].Nodes {
		if node.Kind == "audio" {
			musicIDs[node.SourceTrackID] = true
		}
	}
	f.assertMusicBrowsing(t, musicIDs, albumIDs["music-only"])
	for i, typ := range []string{"asmr", "cd_album", "music", "radio"} {
		var page struct {
			Items []works.Work `json:"items"`
		}
		f.call(t, "GET", "/works?contentType="+typ, nil, "", 200, "Works", &page)
		if len(page.Items) != 1 || page.Items[0].ID != packages[i].WorkID {
			t.Fatalf("%s library returned unexpected works: %+v", typ, page.Items)
		}
		if typ == "asmr" || typ == "radio" {
			item := page.Items[0]
			update := works.UpdateRequest{ExpectedRevision: item.Revision, Title: item.Title, Credits: item.Credits,
				ExternalIDs: item.ExternalIDs, Tags: item.Tags, ShowInTrackList: true}
			f.call(t, "PUT", "/works/"+item.ID, update, "invalid-show-"+typ, 400, "Error", nil)
		}
	}
	cd := packages[1]
	var work works.Work
	f.call(t, "GET", "/works/"+cd.WorkID, nil, "", 200, "Work", &work)
	update := works.UpdateRequest{ExpectedRevision: work.Revision, Title: work.Title, ExternalIDs: work.ExternalIDs,
		Credits: work.Credits, Tags: work.Tags, ShowInTrackList: true}
	f.call(t, "PUT", "/works/"+cd.WorkID, update, "show-disc-tracks", 200, "Work", &work)
	if !work.ShowInTrackList {
		t.Fatal("CD album did not retain track list setting")
	}
	want := map[string]bool{tracks["cd-only"].ID: true}
	for id := range musicIDs {
		want[id] = true
	}
	for _, node := range cd.Nodes {
		if node.Kind == "audio" {
			want[node.SourceTrackID] = true
		}
	}
	var visible library.Page
	f.call(t, "GET", "/source-tracks", nil, "", 200, "SourceTracks", &visible)
	if len(visible.Items) != len(want) {
		t.Fatal("unexpected CD and music track count", visible.Items)
	}
	for _, track := range visible.Items {
		if !want[track.ID] {
			t.Fatal("unexpected track after enabling CD album", track.ID)
		}
	}
	var page catalog.Page
	f.call(t, "GET", "/library/search?type=tracks", nil, "", 200, "CatalogPage", &page)
	if page.Total != len(want) || len(page.Items) != len(want) {
		t.Fatal("catalog query did not include the enabled CD album", page.Total)
	}
	for _, item := range page.Items {
		var track catalog.Track
		if err := json.Unmarshal(item, &track); err != nil || !want[track.ID] {
			t.Fatal("wrong source in track query after enabling CD album", track, err)
		}
	}
	var facets catalog.Facets
	f.call(t, "GET", "/library/facets", nil, "", 200, "CatalogFacets", &facets)
	if len(facets.Genres) != 2 {
		t.Fatal("CD genre did not appear alongside ordinary music", facets.Genres)
	}
	update.ExpectedRevision, update.ShowInTrackList = work.Revision, false
	f.call(t, "PUT", "/works/"+cd.WorkID, update, "hide-disc-tracks", 200, "Work", &work)
	// 重新扫描后普通曲库仍只显示独立音乐。
	f.scan(t, "catalog-rescan-after-packages")
	f.assertMusicBrowsing(t, musicIDs, albumIDs["music-only"])
	f.assertPackageSourcesRemainPlayable(t, packages, tracks, albumIDs)
}

func (f *packageCatalogFixture) assertMusicBrowsing(t *testing.T, musicIDs map[string]bool, musicAlbumID string) {
	t.Helper()
	for _, suffix := range []string{"", "?providerInstanceId=" + packageProvider, "?groupBy=recording", "?groupBy=recording&providerInstanceId=" + packageProvider} {
		var page library.Page
		f.call(t, "GET", "/source-tracks"+suffix, nil, "", 200, "SourceTracks", &page)
		if len(page.Items) != len(musicIDs) || page.NextCursor != nil {
			t.Fatal("ordinary music was removed or pagination was corrupted", suffix)
		}
		for _, track := range page.Items {
			if !musicIDs[track.ID] {
				t.Fatal("work-only audio leaked into track browsing", suffix, track.ID)
			}
			for _, version := range track.Versions {
				if !musicIDs[version.ID] {
					t.Fatal("work-only audio leaked into grouped versions", version.ID)
				}
			}
		}
	}
	for _, suffix := range []string{"", "&providerInstanceId=" + packageProvider} {
		var page catalog.Page
		f.call(t, "GET", "/library/search?type=tracks"+suffix, nil, "", 200, "CatalogPage", &page)
		if page.Total != len(musicIDs) {
			t.Fatal("catalog track count includes work-only audio", page.Total)
		}
		for _, raw := range page.Items {
			var track catalog.Track
			if err := json.Unmarshal(raw, &track); err != nil || !musicIDs[track.ID] {
				t.Fatal("work-only audio leaked into catalog tracks", string(raw), err)
			}
		}
		for _, kind := range []string{"artists", "albums"} {
			f.call(t, "GET", "/library/search?type="+kind+suffix, nil, "", 200, "CatalogPage", &page)
			if page.Total != 1 || len(page.Items) != 1 {
				t.Fatal("work-only entity leaked into unfiltered catalog", kind, page.Total)
			}
			var entity struct {
				Name       string `json:"name"`
				TrackCount int    `json:"trackCount"`
			}
			if err := json.Unmarshal(page.Items[0], &entity); err != nil || entity.Name != "music-only-"+kind[:len(kind)-1] || entity.TrackCount < 1 {
				t.Fatal("catalog entity lost ordinary music", kind, entity, err)
			}
		}
	}
	for _, kind := range []string{"artist", "album", "genre", "folder"} {
		var page catalog.Page
		path := "/library/categories?categoryKind=" + kind
		f.call(t, "GET", path, nil, "", 200, "CatalogPage", &page)
		if kind == "folder" {
			root := requireMusicCategory(t, page, "Root", len(musicIDs))
			f.call(t, "GET", path+"&parentId="+url.QueryEscape(root.ID), nil, "", 200, "CatalogPage", &page)
			requireMusicCategory(t, page, "music-only", len(musicIDs))
		} else {
			requireMusicCategory(t, page, "music-only-"+kind, len(musicIDs))
		}
	}
	for _, suffix := range []string{"", "?providerInstanceId=" + packageProvider} {
		var facets catalog.Facets
		f.call(t, "GET", "/library/facets"+suffix, nil, "", 200, "CatalogFacets", &facets)
		if len(facets.Genres) != 1 || facets.Genres[0].Value != "music-only-genre" || facets.Genres[0].TrackCount != len(musicIDs) || len(facets.Directories) != 2 {
			t.Fatal("work-only categories leaked into facets", facets)
		}
		for _, directory := range facets.Directories {
			if (directory.Name != "Root" && directory.Name != "music-only") || directory.TrackCount != len(musicIDs) {
				t.Fatal("work-only directory leaked into facets", directory)
			}
		}
		var page struct {
			Items []albums.SourceAlbum `json:"items"`
		}
		f.call(t, "GET", "/source-albums"+suffix, nil, "", 200, "SourceAlbums", &page)
		if len(page.Items) != 1 || page.Items[0].ID != musicAlbumID || len(page.Items[0].SourceTrackIDs) != 1 || !musicIDs[page.Items[0].SourceTrackIDs[0]] {
			t.Fatal("work-only album leaked into source album browsing", page.Items)
		}
	}
}

func requireMusicCategory(t *testing.T, page catalog.Page, name string, count int) catalog.Category {
	t.Helper()
	if page.Total != 1 || len(page.Items) != 1 {
		t.Fatal("work-only category leaked into browsing", page.Query.CategoryKind, page.Total)
	}
	var category catalog.Category
	if err := json.Unmarshal(page.Items[0], &category); err != nil || category.Name != name || category.TrackCount != count {
		t.Fatal("ordinary category has incorrect metadata or count", category, name, count, err)
	}
	return category
}

func (f *packageCatalogFixture) assertPackageSourcesRemainPlayable(t *testing.T, packages []workpackages.Package, tracks map[string]library.Track, albumIDs map[string]string) {
	t.Helper()
	otherCookie := f.otherUserCookie(t)
	for _, imported := range packages {
		var result struct {
			Items []workpackages.Package `json:"items"`
		}
		f.call(t, "GET", "/works/"+imported.WorkID+"/packages", nil, "", 200, "WorkPackages", &result)
		if len(result.Items) != 1 || result.Items[0].ID != imported.ID {
			t.Fatal("package disappeared from its work")
		}
		ids := map[string]bool{tracks[imported.Name].ID: true}
		audioCount := 0
		for _, node := range result.Items[0].Nodes {
			if node.Kind == "audio" {
				audioCount++
				if node.Track == nil || node.Track.ID != node.SourceTrackID || node.Track.Availability != "available" {
					t.Fatal("package detail lost its playable source", node)
				}
				ids[node.SourceTrackID] = true
			}
		}
		if audioCount != 1 {
			t.Fatal("package detail lost its audio node", imported.Name, audioCount)
		}
		for id := range ids {
			var direct library.Track
			f.call(t, "GET", "/source-tracks/"+id, nil, "", 200, "SourceTrack", &direct)
			if direct.ID != id || direct.Availability != "available" {
				t.Fatal("direct source lookup lost an explicit work source", direct)
			}
			f.call(t, "GET", "/source-tracks/"+id+"/details", nil, "", 200, "SourceTrackDetails", nil)
			ownerCookie := f.cookie
			f.cookie = otherCookie
			f.call(t, "GET", "/source-tracks/"+id, nil, "", 404, "Error", nil)
			f.cookie = ownerCookie
			var session playback.Description
			f.call(t, "POST", "/playback/sessions", playback.CreateRequest{SourceTrackID: id,
				Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}},
				Preferences:  playback.Preferences{DSPExecution: "client"}}, "play-"+id, 201, "Playback", &session)
			stream := f.call(t, "GET", session.StreamURL, nil, "", 200, "", nil)
			if !bytes.Equal(stream.Body.Bytes(), f.waves[imported.Name]) {
				t.Fatal("explicit source playback did not return the original audio", imported.Name)
			}
			f.call(t, "DELETE", "/playback/sessions/"+session.SessionID, nil, "", 204, "", nil)
		}
		var album albums.SourceAlbum
		f.call(t, "GET", "/source-albums/"+albumIDs[imported.Name], nil, "", 200, "SourceAlbum", &album)
		if len(album.SourceTrackIDs) != 1 || album.SourceTrackIDs[0] != tracks[imported.Name].ID {
			t.Fatal("explicit album lookup lost its original source")
		}
	}
}
