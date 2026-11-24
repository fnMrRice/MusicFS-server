package playback_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func (f *networkPlaybackFixture) next(t *testing.T, key string) playback.Description {
	t.Helper()
	raw, _ := json.Marshal(playback.CreateRequest{SourceTrackID: f.track.ID, Capabilities: playback.Capabilities{HTMLAudio: true, Formats: []playback.Format{{MIME: "audio/wav", CanPlay: "probably"}}}})
	res := f.request(t, http.MethodPost, "/api/v1/playback/sessions", string(raw), map[string]string{"Idempotency-Key": key})
	defer res.Body.Close()
	var d playback.Description
	if res.StatusCode != http.StatusCreated || json.NewDecoder(res.Body).Decode(&d) != nil {
		t.Fatalf("create session: %s", res.Status)
	}
	return d
}

func TestUnversionedNetworkAudioDetectsChangedBytesAndRepairsMissingFile(t *testing.T) {
	f := newNetworkPlaybackFixture(t)
	ctx := t.Context()
	root := t.TempDir()
	m, err := onlinecache.New(ctx, root, f.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: "admin", Plugin: pluginproto.OnlinePluginID(online.Bilibili), Instance: f.track.ProviderID, Source: f.track.ID}
	desc := pluginproto.OnlineAudio{ExternalID: "audio:1", MIME: "audio/wav", Container: "wav", Codec: "pcm_s16le", SizeBytes: 3}
	opens := 0
	read := func(content string) {
		t.Helper()
		body, err := m.Open(ctx, scope, desc, onlinecache.Preferences{LimitBytes: 1024, Stream: false}, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			opens++
			return desc, io.NopCloser(bytes.NewBufferString(content)), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(body)
		body.Close()
		if err != nil || string(b) != content {
			t.Fatalf("audio = %q, %v", b, err)
		}
	}
	read("abc")
	read("abc")
	read("def")
	desc.SizeBytes = 4
	read("defg")
	track, err := f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || len(track.DownloadVersions) != 3 || opens != 4 {
		t.Fatalf("unversioned downloads: %+v, opens=%d, err=%v", track.DownloadVersions, opens, err)
	}
	// 在线浏览页使用外部编号。同一页重复引用、其他账号和同编号的其他实例都不能混入版本。
	for _, page := range []struct {
		owner, instance string
		count           int
	}{
		{scope.Owner, scope.Instance, 3}, {"other", scope.Instance, 0}, {scope.Owner, "other", 0},
	} {
		found, err := f.store.OnlineTrackVersions(ctx, page.owner, page.instance, []string{"audio:1", "audio:2", "audio:1"})
		if err != nil || len(found["audio:1"]) != page.count || len(found["audio:2"]) != 0 {
			t.Fatalf("unexpected versions for %s/%s: %+v, %v", page.owner, page.instance, found, err)
		}
	}
	desc.Version = "known"
	scope.Account, scope.Revision = "account-A", "login-1"
	read("keep")
	track, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil {
		t.Fatal(err)
	}
	id := track.DownloadVersions[0].ID
	if err = os.Remove(filepath.Join(root, id+".audio")); err != nil {
		t.Fatal(err)
	}
	read("keep")
	scope.Revision = "login-2"
	read("keep")
	if opens != 6 {
		t.Fatalf("missing stable file was not repaired and reused: %d", opens)
	}
	desc.Version = ""
	upstream := bytes.NewBufferString("keep")
	body, err := m.Open(ctx, scope, desc, onlinecache.Preferences{LimitBytes: 1024, Stream: true}, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
		actual := desc
		actual.Version = "known"
		return actual, io.NopCloser(upstream), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(body)
	body.Close()
	if err != nil || string(b) != "keep" || upstream.Len() != 4 {
		t.Fatal("late strong validator did not reuse cached media bytes")
	}
	scope.Account = "account-B"
	desc.Version = "known"
	read("only")
	if opens != 7 {
		t.Fatal("download reused across platform accounts")
	}
}

func (f *networkPlaybackFixture) consume(t *testing.T, d playback.Description) {
	t.Helper()
	res := f.request(t, http.MethodGet, d.StreamURL, "", nil)
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || len(b) <= 1024 {
		t.Fatalf("read complete audio: %s, %d, %v", res.Status, len(b), err)
	}
}

func TestNetworkDownloadsRetainVersionsReuseAndTrim(t *testing.T) {
	f := newNetworkPlaybackFixture(t, true)
	f.consume(t, f.description)
	replay := f.next(t, "cache-repeat")
	if replay.OutputProfilePending || replay.OutputProfile.SampleRate != 48000 || replay.OutputProfile.Channels != 2 ||
		replay.OutputProfile.BitDepth != 16 || replay.OutputProfile.BitrateBps != 1536000 {
		t.Fatalf("completed file format missing before playback: %+v", replay.OutputProfile)
	}
	f.consume(t, replay)
	if f.provider.opens.Load() != 1 {
		t.Fatal("cache hit opened media")
	}
	plugin := pluginproto.OnlinePluginID(online.Bilibili)
	if _, err := f.store.PutPluginSettings(t.Context(), "admin", plugin, 0, settings.Values{"playbackQuality": "lossless", "streamWhileDownloading": false}); err != nil {
		t.Fatal(err)
	}
	second := f.next(t, "cache-second-version")
	if !second.OutputProfilePending || second.OutputProfile.SampleRate != 0 || second.OutputProfile.BitDepth != 0 {
		t.Fatalf("new quality inherited the previous download's format: %+v", second.OutputProfile)
	}
	f.consume(t, second)
	response := f.request(t, http.MethodGet, "/api/v1/playback/sessions/"+second.SessionID+"/attempts/"+second.AttemptID+"/output-profile", "", nil)
	defer response.Body.Close()
	var measured playback.OutputProfile
	if response.StatusCode != http.StatusOK || json.NewDecoder(response.Body).Decode(&measured) != nil ||
		measured.SampleRate != 48000 || measured.Channels != 2 || measured.BitDepth != 16 {
		t.Fatalf("late file format missing from the playback attempt: %+v", measured)
	}
	track, err := f.store.FindTrack(t.Context(), "admin", f.track.ID)
	if err != nil || !track.Downloaded || len(track.DownloadVersions) != 2 || f.provider.opens.Load() != 2 {
		t.Fatalf("independent audio versions: %+v, %v", track.DownloadVersions, err)
	}
	if track.DownloadVersions[0].Quality != "lossless" || track.DownloadVersions[0].ID == track.DownloadVersions[1].ID {
		t.Fatal("quality or version identity was lost")
	}
	music, err := f.store.MusicTrackForSource(t.Context(), "admin", f.track.ID)
	if err != nil || len(music.Sources) != 1 || len(music.Sources[0].DownloadVersions) != 2 {
		t.Fatalf("catalog source versions: %+v, %v", music.Sources, err)
	}
	for _, v := range music.Sources[0].DownloadVersions {
		if !v.FormatMeasured || v.SampleRateHz != 48000 || v.Channels != 2 || v.BitDepth != 16 {
			t.Fatalf("catalog lost the measured version format: %+v", v)
		}
	}
	restored, err := onlinecache.New(t.Context(), filepath.Join(f.cacheRoot, "playback", "online"), f.store)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PutPluginSettings(t.Context(), "admin", plugin, 1, settings.Values{"cacheLimitMiB": 0, "streamWhileDownloading": false}); err != nil {
		t.Fatal(err)
	}
	if err = restored.Trim(t.Context()); err != nil {
		t.Fatal(err)
	}
	track, err = f.store.FindTrack(t.Context(), "admin", f.track.ID)
	if err != nil || track.Downloaded || len(track.DownloadVersions) != 0 {
		t.Fatalf("trimmed files still advertised: %+v, %v", track.DownloadVersions, err)
	}
	f.consume(t, f.next(t, "cache-inactive"))
	if rows, err := f.store.ListOnlineDownloads(t.Context()); err != nil || len(rows) != 0 {
		t.Fatalf("zero cache retained file: %v, %v", rows, err)
	}
}

func TestBufferedNetworkDownloadWaitsForCompleteFile(t *testing.T) {
	f := newNetworkPlaybackFixture(t)
	plugin := pluginproto.OnlinePluginID(online.Bilibili)
	if _, err := f.store.PutPluginSettings(t.Context(), "admin", plugin, 0, settings.Values{"streamWhileDownloading": false}); err != nil {
		t.Fatal(err)
	}
	d := f.next(t, "cache-buffered-session")
	responses := make(chan *http.Response, 1)
	go func() { responses <- f.request(t, http.MethodGet, d.StreamURL, "", nil) }()
	deadline := time.After(4 * time.Second)
	for f.provider.opens.Load() == 0 {
		select {
		case <-deadline:
			close(f.release)
			t.Fatal("buffered download did not start")
		case <-time.After(5 * time.Millisecond):
		}
	}
	select {
	case res := <-responses:
		close(f.release)
		res.Body.Close()
		t.Fatal("buffered playback sent headers before the upstream completed")
	case <-time.After(75 * time.Millisecond):
	}
	track, err := f.store.FindTrack(t.Context(), "admin", f.track.ID)
	if err != nil || track.Downloaded {
		t.Errorf("partial file was published: %v", err)
	}
	close(f.release)
	res := <-responses
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != http.StatusOK || len(b) <= 1024 {
		t.Fatalf("buffered response: %s, %d, %v", res.Status, len(b), err)
	}
}

func TestDailyOnlineCacheCleanupRetainsNewestWithinLimit(t *testing.T) {
	f := newNetworkPlaybackFixture(t)
	ctx := t.Context()
	plugin := pluginproto.OnlinePluginID(online.Bilibili)
	if _, err := f.store.PutPluginSettings(ctx, "admin", plugin, 0, settings.Values{"cacheLimitMiB": 1}); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	m, err := onlinecache.New(ctx, root, f.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: "admin", Plugin: plugin, Instance: f.track.ProviderID, Source: f.track.ID}
	data := make([]byte, 600<<10)
	for _, quality := range []string{"standard", "lossless"} {
		desc := pluginproto.OnlineAudio{ExternalID: "audio:1", Quality: quality, Version: "fixture", MIME: "audio/wav", Container: "wav", Codec: "pcm_s16le", SizeBytes: int64(len(data))}
		handle, err := m.Open(ctx, scope, desc, onlinecache.Preferences{LimitBytes: 1 << 20}, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			return desc, io.NopCloser(bytes.NewReader(data)), nil
		})
		if err != nil {
			t.Fatal(err)
		}
		handle.Close()
	}
	if _, err = onlinecache.New(ctx, root, f.store); err != nil {
		t.Fatal(err)
	}
	state, err := f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || len(state.DownloadVersions) != 2 {
		t.Fatalf("restart repeated same-day cleanup: %v", err)
	}
	if err = os.WriteFile(filepath.Join(root, "last-cleanup"), []byte(time.Now().AddDate(0, 0, -1).Format(time.DateOnly)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = onlinecache.New(ctx, root, f.store); err != nil {
		t.Fatal(err)
	}
	state, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || len(state.DownloadVersions) != 1 || state.DownloadVersions[0].Quality != "lossless" {
		t.Fatalf("next daily cleanup did not retain newest: %+v, %v", state.DownloadVersions, err)
	}
}
