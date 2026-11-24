package enrichment_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/scraper"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichment"
	"github.com/fnMrRice/MusicFS-server/src/metadata/enrichmentworker"
	"github.com/fnMrRice/MusicFS-server/src/metadata/onlinescrape"
	"github.com/fnMrRice/MusicFS-server/tests/testkit"
)

const (
	refreshNetEase = "musicfs.scraper.netease"
	refreshQQ      = "musicfs.scraper.qqmusic"
)

type refreshLyricsCall struct {
	plugin, field string
	query         scraper.Query
}

type refreshLyricsPlugin struct {
	id    string
	calls chan<- refreshLyricsCall
}

func (p refreshLyricsPlugin) Descriptor() scraper.Descriptor {
	return scraper.Descriptor{PluginID: p.id, Type: "scraper", Name: p.id, Fields: []string{"lyrics"}, Available: true}
}

func (p refreshLyricsPlugin) Search(ctx context.Context, field string, q scraper.Query) ([]scraper.Item, error) {
	select {
	case p.calls <- refreshLyricsCall{plugin: p.id, field: field, query: q}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	item := scraper.Item{ID: "found", Title: q.Title, Artist: q.Artist, Format: "lrc", Lyrics: "[00:01]QQ lyrics for " + q.Title}
	if p.id == refreshNetEase {
		// 同名但不同艺人的歌词不得保存到此曲目，必须继续查下一个来源。
		item.Artist, item.Lyrics = "Different artist", "[00:01]Wrong recording"
	}
	return []scraper.Item{item}, nil
}

func TestDirectoryRefreshHonorsLyricsOrderAndPublishesArtwork(t *testing.T) {
	for _, tc := range []struct {
		name          string
		order         []string
		localDisabled bool
	}{
		{name: "default"},
		{name: "QQ before local", order: []string{refreshQQ, onlinescrape.LocalLyricsPluginID, refreshNetEase}},
		{name: "local scraper disabled", order: []string{onlinescrape.LocalLyricsPluginID, refreshQQ, refreshNetEase}, localDisabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := setup(t)
			ctx := t.Context()
			calls := make(chan refreshLyricsCall, 16)
			catalog, err := onlinescrape.NewWithSettings([]scraper.Plugin{refreshLyricsPlugin{refreshNetEase, calls}, refreshLyricsPlugin{refreshQQ, calls}}, f.store)
			if err != nil {
				t.Fatal(err)
			}
			if tc.order != nil {
				rev := int64(0)
				if _, err = catalog.SaveLyricsAutoSettings(ctx, "admin", onlinescrape.LyricsAutoSettingsInput{ExpectedRevision: &rev, PluginOrder: tc.order}, domain.NewID()); err != nil {
					t.Fatal(err)
				}
			}
			if tc.localDisabled {
				// 文件来源继续工作，仅保留没有运行宿主的本地刮削实例。
				f.service, err = enrichmentworker.New(f.store, f.lib, f.content, f.runtimes[1:], enrichmentworker.Config{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(f.service.Close)
			}
			f.service.LyricsCatalog = catalog
			localTitle, remoteTitle := "Song with sidecar", "Song without lyrics"
			artist := "Fixture artist"
			localLyrics := []byte("[00:01]Local sidecar lyrics")
			covers := map[string][]byte{"good.mp3": resolutionArtwork(t, 16, 16, 8), "other.mp3": resolutionArtwork(t, 32, 32, 9)}
			for path, body := range map[string][]byte{
				"good.lrc": localLyrics, "good.png": covers["good.mp3"], "other.png": covers["other.mp3"],
				"other.mp3": testkit.ScraperID3(4, testkit.ScraperFrame{ID: "TIT2", Body: append([]byte{3}, remoteTitle...)}),
			} {
				if err = os.WriteFile(filepath.Join(f.root, path), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			plan := testkit.ScanPlan("root")
			plan.Mode, plan.RefreshMetadata = "full", true
			scan := rescanAutomaticPlanWithTags(t, f, plan, map[string]library.Observation{
				"good.mp3": {Title: &localTitle, Artists: []string{artist}}, "other.mp3": {Title: &remoteTitle, Artists: []string{artist}},
			})
			if err = f.service.Start(ctx); err != nil {
				t.Fatal(err)
			}
			results := automaticFinished(t, f, scan.ID)
			f.service.Close()
			if len(results) != 2 {
				t.Fatalf("expected both tracks in refresh: %+v", results)
			}
			for _, result := range results {
				if result.State != "completed" || result.Reason != "refreshMetadata" {
					t.Fatalf("refresh did not finish: %+v", result)
				}
				if tc.localDisabled && !slices.ContainsFunc(result.Actions, func(a enrichment.AutomationAction) bool {
					return a.Field == "artwork" && a.Status == "failed" && (a.Reason == "SOURCE_OFFLINE" || a.Reason == "UNSUPPORTED" || a.Reason == "PLUGIN_DISABLED")
				}) {
					t.Fatalf("missing artwork provider diagnostic: %+v", result.Actions)
				}
			}
			close(calls)
			searched := make(map[string][]string)
			for call := range calls {
				if call.field != "lyrics" || call.query.Artist != artist || (call.query.Title != localTitle && call.query.Title != remoteTitle) {
					t.Fatalf("unexpected catalog lookup: %+v", call)
				}
				searched[call.query.Title] = append(searched[call.query.Title], call.plugin)
			}
			var wantLocal []string
			wantRemote := []string{refreshNetEase, refreshQQ}
			if tc.order != nil {
				wantLocal, wantRemote = []string{refreshQQ}, []string{refreshQQ}
			}
			if !slices.Equal(searched[localTitle], wantLocal) || !slices.Equal(searched[remoteTitle], wantRemote) {
				t.Fatalf("unexpected lookup order by track: %+v", searched)
			}
			for _, track := range f.tracks {
				isLocal := track.ID == f.good().ID
				title := remoteTitle
				if isLocal {
					title = localTitle
				}
				expectedLyrics := []byte("[00:01]QQ lyrics for " + title)
				lyricsInstance := refreshQQ
				if isLocal && tc.order == nil {
					expectedLyrics, lyricsInstance = localLyrics, f.runtimes[0].ID
				}
				assertRefreshContent(t, f, track.ID, "lyrics", "und", lyricsInstance, expectedLyrics)
				if !tc.localDisabled {
					assertRefreshContent(t, f, track.ID, "artwork", "", f.runtimes[0].ID, covers[track.DisplayName])
				}
			}
		})
	}
}

func assertRefreshContent(t *testing.T, f fixture, track, field, language, instance string, want []byte) {
	t.Helper()
	selection, err := f.content.ContentSelection(t.Context(), "admin", track)
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range selection.Bindings {
		if binding.Field != field || binding.Language != language {
			continue
		}
		obj, reader, err := f.content.Open(t.Context(), "admin", binding.ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		body, err := io.ReadAll(reader)
		if err != nil || !bytes.Equal(body, want) || obj.Metadata.Origin.InstanceID != instance || !binding.Current || binding.ManualLocked || obj.Metadata.Target != selection.Target || binding.Target != selection.Target {
			t.Fatalf("incorrect %s for track %s: object=%+v binding=%+v matchesBytes=%t error=%v", field, track, obj.Metadata, binding, bytes.Equal(body, want), err)
		}
		return
	}
	t.Fatalf("missing %s/%s for %s: %+v", field, language, track, selection)
}
