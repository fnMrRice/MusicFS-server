package storage_test

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/audiotypes"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/library/jobs"
	"github.com/fnMrRice/MusicFS-server/src/library/music"
	"github.com/fnMrRice/MusicFS-server/src/library/works"
	"github.com/fnMrRice/MusicFS-server/src/metadata/matching"
	"github.com/fnMrRice/MusicFS-server/src/storage"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

type albumImportFixture struct {
	t     *testing.T
	store storage.Backend
	db    *sql.DB
	scans int
}

func releaseFile(id, album, songID, releaseID string) library.Observation {
	o := evidenceObservation(id, "v1")
	title := "Song " + id
	o.Title, o.Album, o.Artists, o.ProbeStatus = &title, &album, []string{"Artist"}, "succeeded"
	if releaseID != "" {
		o.NetEaseID = songID
		o.AlbumRelease = &audiotypes.AlbumRelease{Namespace: "netease", ID: releaseID}
	}
	return o
}

func (f *albumImportFixture) start(owner, provider string) jobs.Job {
	f.t.Helper()
	f.scans++
	ctx := f.t.Context()
	enqueued, _, err := f.store.Enqueue(ctx, owner, provider, testkit.ScanPlan("music"), fmt.Sprintf("release-scan-%d", f.scans))
	if err != nil {
		f.t.Fatal(err)
	}
	job, err := f.store.Claim(ctx)
	if err != nil || job.ID != enqueued.ID {
		f.t.Fatalf("Claimed %q instead of %q: %v", job.ID, enqueued.ID, err)
	}
	return job
}

func (f *albumImportFixture) scan(provider string, items ...library.Observation) {
	f.t.Helper()
	job := f.start("admin", provider)
	if err := f.store.Publish(f.t.Context(), job, testkit.ScanPublication(job, items)); err != nil {
		f.t.Fatal(err)
	}
	if err := f.store.Finish(f.t.Context(), job, len(items), nil); err != nil {
		f.t.Fatal(err)
	}
}

func (f *albumImportFixture) source(provider, externalID string) string {
	f.t.Helper()
	var id string
	if err := f.db.QueryRowContext(f.t.Context(), "SELECT id FROM tracks WHERE owner='admin' AND provider=$1 AND external_id=$2", provider, externalID).Scan(&id); err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *albumImportFixture) album(source string) string {
	f.t.Helper()
	track, err := f.store.MusicTrackForSource(f.t.Context(), "admin", source)
	if err != nil || len(track.AlbumIDs) != 1 {
		f.t.Fatalf("Unexpected albums for source %q: %v, %v", source, track.AlbumIDs, err)
	}
	return track.AlbumIDs[0]
}

func (f *albumImportFixture) exec(query string, args ...any) {
	f.t.Helper()
	if _, err := f.db.ExecContext(f.t.Context(), query, args...); err != nil {
		f.t.Fatal(err)
	}
}

func TestMusicAlbumNativeReleaseImport(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		f := albumImportFixture{t: t, store: store, db: db}
		ctx := t.Context()
		if err := store.BindMusicSourcePlugins(ctx, map[string]string{"cloud": pluginproto.OnlinePluginID(online.Netease), "qq": pluginproto.OnlinePluginID(online.QQMusic)}); err != nil {
			t.Fatal(err)
		}
		publish := func(owner, provider, song, album, release string) string {
			t.Helper()
			meta := catalogOnline(song, "Song first")
			meta.Album, meta.AlbumRelease = album, &audiotypes.AlbumRelease{Namespace: "netease", ID: release}
			meta.SourceURL = "https://music.163.com/song?id=" + song
			if provider == "qq" {
				meta.AlbumRelease.Namespace = "qqmusic"
				meta.SourceURL = "https://y.qq.com/n/ryqq/songDetail/" + song
			}
			source, err := store.PublishOnlineTrack(ctx, owner, provider, meta)
			if err != nil {
				t.Fatal(err)
			}
			return source.ID
		}
		// 同一实例的同名发行及同一首歌收录于不同发行，均不能按文字合并。
		a := releaseFile("first", "Common", "11", "100")
		b := releaseFile("second", "Common", "12", "101")
		b.Title = a.Title
		f.scan("files", a, b)
		first, second := f.source("files", "first"), f.source("files", "second")
		releaseA, releaseB := f.album(first), f.album(second)
		if releaseA == releaseB {
			t.Fatal("Two native releases and their songs share one album")
		}
		cloud := publish("admin", "cloud", "13", "Alternate spelling", "100")
		if f.album(cloud) != releaseA {
			t.Fatal("Cloud and file releases with the same native identity diverged")
		}
		if f.album(publish("admin", "qq", "14", "Common", "100")) == releaseA {
			t.Fatal("Across-platform numeric collision reused a different release")
		}
		other, err := store.MusicTrackForSource(ctx, "other", publish("other", "cloud", "13", "Alternate spelling", "100"))
		if err != nil || len(other.AlbumIDs) != 1 || other.AlbumIDs[0] == releaseA {
			t.Fatalf("Release crossed owners: %v, %v", other.AlbumIDs, err)
		}
		// 同一份文件字节却第二次报告不同歌曲号。保留原先归属，同时撤回该观测的发行声明。
		conflict := a
		conflict.NetEaseID = "999"
		conflict.AlbumRelease = &audiotypes.AlbumRelease{Namespace: "netease", ID: "999"}
		f.scan("files", conflict, b)
		observed, err := store.FindTrack(ctx, "admin", first)
		if err != nil || observed.AlbumRelease != nil || f.album(first) != releaseA {
			t.Fatalf("Conflicting song IDs on one asset asserted a release: %+v, %v", observed.AlbumRelease, err)
		}

		// 原始标签更正之后，仅该音源换发行；同一标题重复重扫应指向同一张。
		a.ObservedVersion = "v2"
		a.AlbumRelease = &audiotypes.AlbumRelease{Namespace: "netease", ID: "101"}
		f.scan("files", a, b)
		f.scan("files", a, b)
		if f.album(first) != releaseB || f.album(second) != releaseB || f.album(cloud) != releaseA {
			t.Fatal("Native release change or repeated scan moved unrelated sources")
		}
		link, err := store.ManualLink(ctx, "admin", matching.LinkRequest{SourceTrackIDs: []string{first, cloud}}, "two-releases")
		if err != nil || link.RecordingID == nil {
			t.Fatalf("Could not link the shared recording: %+v %v", link, err)
		}
		shared, err := store.MusicTrack(ctx, "admin", *link.RecordingID)
		if err != nil || len(shared.AlbumIDs) != 2 || !slices.Contains(shared.AlbumIDs, releaseA) || !slices.Contains(shared.AlbumIDs, releaseB) {
			t.Fatalf("Release memberships lost on a shared recording: %v, %v", shared.AlbumIDs, err)
		}
	})
}

func TestMusicAlbumNativeReleasePartialEvidence(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		f := albumImportFixture{t: t, store: store, db: db}
		ctx := t.Context()
		pending, unproven := releaseFile("one", "Common", "", ""), releaseFile("two", "Common", "", "")
		f.scan("files", pending, unproven)
		first, second := f.source("files", "one"), f.source("files", "two")
		old := f.album(first)
		if f.album(second) != old {
			t.Fatal("Legacy tag fixture was not grouped by its instance")
		}
		legacy, err := store.MusicAlbum(ctx, "admin", old)
		if err != nil {
			t.Fatal(err)
		}
		publisher := "Manually verified publisher"
		legacy.Publisher = &publisher
		saved, err := store.SaveMusicAlbum(ctx, "admin", old, legacy.Revision, legacy.AlbumFields, "save-legacy-fields")
		if err != nil {
			t.Fatal(err)
		}
		pending = releaseFile("one", "Common", "31", "300")
		f.scan("files", pending, unproven)
		if f.album(first) == old || f.album(second) != old {
			t.Fatal("One proven track claimed unrelated members of an existing album")
		}
		preserved, err := store.MusicAlbum(ctx, "admin", old)
		if err != nil || preserved.Publisher == nil || *preserved.Publisher != publisher || preserved.Name != saved.Name {
			t.Fatalf("Manual album fields changed: %+v, %v", preserved, err)
		}
		// 同实例第三首新歌恰好具有旧卡曾刮到的发行号，不能仅凭同名认领原卡中的未识别歌曲。
		details := works.ScrapedField{Field: "albumDetails", PluginID: "musicfs.scraper.netease", CandidateToken: "details",
			Item: scraper.Item{ID: "301", Title: "Common"}}
		if _, err = store.SelectMusicAlbumScrapedField(ctx, "admin", old, preserved.Revision, details, "unknown-legacy-catalog"); err != nil {
			t.Fatal(err)
		}
		f.scan("files", pending, unproven, releaseFile("third", "Common", "32", "301"))
		if f.album(f.source("files", "third")) == old || f.album(second) != old {
			t.Fatal("An unrelated new song claimed the old same-name album from its scraped catalog alone")
		}
	})
}

func TestMusicAlbumNativeReleaseAtomicPublication(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		f := albumImportFixture{t: t, store: store, db: db}
		ctx := t.Context()
		// 第二项证据非法时，第一项已触发的发行认领须同整批回滚。
		j := f.start("admin", "rollback")
		valid, invalid := releaseFile("one", "Shared", "41", "400"), releaseFile("two", "Second", "42", "401")
		invalid.ProbeStatus = "failed"
		if err := store.Publish(ctx, j, testkit.ScanPublication(j, []library.Observation{valid, invalid})); err == nil {
			t.Fatal("Invalid probe evidence was accepted")
		}
		for _, table := range []string{"tracks", "music_albums", "music_album_release_identities"} {
			var count int
			if err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil || count != 0 {
				t.Fatalf("Partial publication in %s: %d %v", table, count, err)
			}
		}
		// 独立的来源作业同时重新发布同一发行编号。两端均须取得同一个规范专辑。
		jobsToPublish := []jobs.Job{j, f.start("admin", "another")}
		start := make(chan struct{})
		errs := make(chan error, len(jobsToPublish))
		for _, job := range jobsToPublish {
			go func() {
				<-start
				errs <- store.Publish(ctx, job, testkit.ScanPublication(job, []library.Observation{valid}))
			}()
		}
		close(start)
		for range jobsToPublish {
			if err := <-errs; err != nil {
				t.Error(err)
			}
		}
		if t.Failed() {
			return
		}
		for _, job := range jobsToPublish {
			if err := store.Finish(ctx, job, 1, nil); err != nil {
				t.Fatal(err)
			}
		}
		var cards, ids int
		if err := db.QueryRowContext(ctx, "SELECT (SELECT count(*) FROM music_albums),(SELECT count(*) FROM music_album_release_identities)").Scan(&cards, &ids); err != nil || cards != 1 || ids != 1 {
			t.Fatalf("Concurrent publication left %d albums and %d identities: %v", cards, ids, err)
		}
		if f.album(f.source("another", "one")) != f.album(f.source("rollback", "one")) {
			t.Fatal("Concurrent imports diverged")
		}
	})
}

func TestMusicAlbumNativeReleaseKeepsVerifiedLegacyCatalog(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		ctx := t.Context()
		f := albumImportFixture{t: t, store: store, db: db}
		for _, tc := range []struct {
			file, nativeID, savedID, scraper string
			retain                           bool
		}{
			{"cross-platform", "501", "other-501", "musicfs.scraper.bangumi", true},
			{"same-platform-conflict", "502", "999", "musicfs.scraper.netease", false},
			{"new-source", "503", "other-503", "musicfs.scraper.bangumi", true},
		} {
			album := "Legacy " + tc.file
			noID := releaseFile(tc.file, album, "", "")
			f.scan(tc.file, noID)
			sid := f.source(tc.file, tc.file)
			legacy := f.album(sid)
			if tc.file == "new-source" {
				f.exec("INSERT INTO music_album_tag_aliases(owner,provider,tag_name,album_id) VALUES('admin','old-mirror',$1,$2)", album, legacy)
				f.scan("old-mirror", releaseFile("copy", album, "", ""))
				if f.album(f.source("old-mirror", "copy")) != legacy {
					t.Fatal("Two original instances must already share the explicitly merged album")
				}
			}
			previous, err := store.MusicAlbum(ctx, "admin", legacy)
			if err != nil {
				t.Fatal(err)
			}
			previous.SourceLinks = []music.AlbumSourceLink{{PluginID: "musicfs.scraper.netease", URL: "https://music.163.com/album?id=" + tc.nativeID}}
			previous, err = store.SaveMusicAlbum(ctx, "admin", legacy, previous.Revision, previous.AlbumFields, "known-link-"+tc.file)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := works.ScrapedField{Field: "albumTracks", PluginID: tc.scraper, CandidateToken: "preview", Item: scraper.Item{ID: tc.savedID, Title: album, Tracks: []scraper.Track{{Title: *noID.Title, Artist: "Artist"}}}}
			if _, err = store.SelectMusicAlbumScrapedField(ctx, "admin", legacy, previous.Revision, snapshot, "known-catalog-"+tc.file); err != nil {
				t.Fatal(err)
			}
			// 新实例可以比两个既有成员更早读到编号。即使其专辑标签拼写不同，也应直接找回明确合并的旧卡。
			if tc.file == "new-source" {
				f.scan("third-instance", releaseFile("third", "Translated name", "52", tc.nativeID))
				sid = f.source("third-instance", "third")
			} else {
				f.scan(tc.file, releaseFile(tc.file, album, "51", tc.nativeID))
			}
			// 已手工选用的曲目表可以继续引用这首录音，单独检查本次导入产生的真实来源归属。
			var got string
			if err = db.QueryRowContext(ctx, "SELECT album_id FROM music_album_source_refs WHERE origin_id='tag' AND source_id=$1", sid).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if (got == legacy) != tc.retain {
				t.Fatalf("Wrong native album for %s: %q, previous %q", tc.file, got, legacy)
			}
			saved, err := store.MusicAlbumScrapedFields(ctx, "admin", legacy)
			if err != nil || len(saved) != 1 || saved[0].PluginID != tc.scraper || saved[0].Item.ID != tc.savedID {
				t.Fatalf("Saved edition changed for %s: %v, %v", tc.file, saved, err)
			}
		}
	})
}

func TestMusicAlbumNativeReleaseLateEvidenceAndOverrides(t *testing.T) {
	testAlbumBackends(t, func(t *testing.T, store storage.Backend, db *sql.DB) {
		f := albumImportFixture{t: t, store: store, db: db}
		ctx := t.Context()
		pending := releaseFile("pending", "Legacy", "", "")
		f.scan("files", pending)
		sid := f.source("files", "pending")
		legacy := f.album(sid)
		f.scan("untagged", pending)
		unrelated := f.album(f.source("untagged", "pending"))
		if unrelated == legacy {
			t.Fatal("Unidentified source albums merged by title across instances")
		}
		proven := releaseFile("pending", "Legacy", "22", "200")
		f.scan("files", proven)
		if f.album(sid) != legacy {
			t.Fatal("Late native evidence replaced a compatible single-member album ID")
		}
		// 原音源的新版本暂时没有发行编号，保留已有归属。另一个同实例同名音源不能据此进入已认领的卡。
		pending.ObservedVersion = "v2"
		fresh := releaseFile("new-file", "Legacy", "", "")
		f.scan("files", pending, fresh)
		fallback := f.album(f.source("files", "new-file"))
		if f.album(sid) != legacy || fallback == legacy || fallback == unrelated {
			t.Fatal("A new unproven source entered a known release or the previous member lost its album")
		}
		proven.ObservedVersion = "v3"
		f.scan("files", proven, fresh)
		// 旧整标签别名不能压过明确异版。新来的无编号来源同样不能凭名字进入已经核实的卡。
		f.exec("INSERT INTO music_album_tag_aliases(owner,provider,tag_name,album_id) VALUES('admin','secondary','Legacy',$1)", legacy)
		unknown, different := releaseFile("untagged", "Legacy", "", ""), releaseFile("other", "Legacy", "23", "201")
		f.scan("secondary", unknown, different)
		dst := f.album(f.source("secondary", "other"))
		ungrouped := f.album(f.source("secondary", "untagged"))
		if ungrouped == legacy || ungrouped == dst || dst == legacy || dst == unrelated {
			t.Fatal("An explicit release followed a conflicting provider alias")
		}
		manual, err := store.SaveMusicAlbum(ctx, "admin", "", 0, music.AlbumFields{Name: "Legacy"}, "manual-release")
		if err != nil {
			t.Fatal(err)
		}
		manualTrack, err := store.SaveMusicTrack(ctx, "admin", "", 0, music.TrackInput{TrackFields: music.TrackFields{Title: "Manual only"}, ArtistNames: []string{"Artist"}, AlbumIDs: []string{manual.ID}}, "manual-track")
		if err != nil {
			t.Fatal(err)
		}
		f.exec("INSERT INTO music_album_source_tag_overrides(owner,source_id,tag_name,album_id) VALUES('admin',$1,'Legacy',$2)", sid, manual.ID)
		proven.ObservedVersion = "v4"
		editedTitle := "Updated source title"
		proven.Title = &editedTitle
		mirror := releaseFile("mirror", "Legacy", "24", "200")
		f.scan("files", proven, mirror, fresh)
		f.scan("files", proven, mirror, fresh)
		if f.album(sid) != manual.ID || f.album(f.source("files", "mirror")) != legacy || f.album(f.source("secondary", "other")) != dst {
			t.Fatal("An explicit source override was lost or propagated to another track")
		}
		got, err := store.MusicTrack(ctx, "admin", manualTrack.ID)
		if err != nil || got.Revision != manualTrack.Revision || got.Title != manualTrack.Title || !slices.Equal(got.AlbumIDs, []string{manual.ID}) {
			t.Fatalf("Unrelated manual recording changed: %+v, %v", got, err)
		}
	})
}
