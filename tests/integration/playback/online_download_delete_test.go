package playback_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/playback/onlinecache"
	"github.com/fnMrRice/MusicFS-server/src/plugins/settings"
)

func TestDeleteOnlineDownloadPreservesOtherQualityAndTrafficSavings(t *testing.T) {
	f := newNetworkPlaybackFixture(t, true)
	ctx := t.Context()
	plugin := pluginproto.OnlinePluginID(online.Bilibili)
	setQuality := func(revision int64, quality string) {
		t.Helper()
		if _, err := f.store.PutPluginSettings(ctx, "admin", plugin, revision, settings.Values{"playbackQuality": quality}); err != nil {
			t.Fatal(err)
		}
	}
	setQuality(0, "standard")
	f.consume(t, f.next(t, "download-standard"))
	setQuality(1, "lossless")
	f.consume(t, f.next(t, "download-lossless"))
	track, err := f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || len(track.DownloadVersions) != 2 {
		t.Fatalf("versions = %+v, %v", track.DownloadVersions, err)
	}
	ids := map[string]string{}
	for _, v := range track.DownloadVersions {
		ids[v.Quality] = v.ID
	}
	remove := func(source, id string, headers map[string]string, want int) {
		t.Helper()
		res := f.request(t, http.MethodDelete, "/api/v1/source-tracks/"+source+"/downloads/"+id, "", headers)
		defer res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("delete version: %s, want %d", res.Status, want)
		}
	}
	remove(f.track.ID, ids["standard"], map[string]string{"X-CSRF-Token": "wrong"}, http.StatusForbidden)
	remove("wrong-source", ids["standard"], nil, http.StatusNotFound)
	remove(f.track.ID, ids["standard"], nil, http.StatusNoContent)
	remove(f.track.ID, ids["standard"], nil, http.StatusNotFound)
	track, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || !track.Downloaded || len(track.DownloadVersions) != 1 || track.DownloadVersions[0].Quality != "lossless" {
		t.Fatalf("remaining source qualities = %+v, %v", track.DownloadVersions, err)
	}
	f.consume(t, f.next(t, "download-remaining-hit"))
	if f.provider.opens.Load() != 2 {
		t.Fatal("remaining cached quality requested upstream audio")
	}
	remove(f.track.ID, ids["lossless"], nil, http.StatusNoContent)
	track, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || track.Downloaded || len(track.DownloadVersions) != 0 {
		t.Fatalf("source still advertises deleted audio: %+v, %v", track.DownloadVersions, err)
	}
	f.consume(t, f.next(t, "download-after-deletion"))
	if f.provider.opens.Load() != 3 {
		t.Fatal("playing a removed quality did not fetch a new copy")
	}
}

func TestDeleteOnlineDownloadWithActiveReadersAndDownload(t *testing.T) {
	f := newNetworkPlaybackFixture(t)
	ctx := t.Context()
	root := t.TempDir()
	m, err := onlinecache.New(ctx, root, f.store)
	if err != nil {
		t.Fatal(err)
	}
	scope := onlinecache.Scope{Owner: "admin", Plugin: pluginproto.OnlinePluginID(online.Bilibili), Instance: f.track.ProviderID, Source: f.track.ID}
	desc := pluginproto.OnlineAudio{ExternalID: "audio:1", MIME: "audio/wav", Codec: "pcm_s16le", Container: "wav", SizeBytes: 3}
	p := onlinecache.Preferences{LimitBytes: 1024}
	read := func() (io.ReadCloser, error) {
		return m.Open(ctx, scope, desc, p, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			return desc, io.NopCloser(bytes.NewBufferString("abc")), nil
		})
	}
	old, err := read()
	if err != nil {
		t.Fatal(err)
	}
	defer old.Close()
	track, err := f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || len(track.DownloadVersions) != 1 {
		t.Fatalf("cached source: %v", err)
	}
	id := track.DownloadVersions[0].ID
	if err = m.Delete(ctx, "other-user", scope.Source, id); pluginproto.ErrorCode(err) != "NOT_FOUND" {
		t.Fatalf("cross-user delete: %v", err)
	}
	if _, err = f.store.DB.ExecContext(ctx, "CREATE TEMP TRIGGER fail_cache_delete BEFORE DELETE ON online_downloads BEGIN SELECT RAISE(ABORT,'injected delete failure'); END"); err != nil {
		t.Fatal(err)
	}
	if err = m.Delete(ctx, scope.Owner, scope.Source, id); err == nil {
		t.Fatal("failed database delete reported success")
	}
	if _, err = f.store.DB.ExecContext(ctx, "DROP TRIGGER fail_cache_delete"); err != nil {
		t.Fatal(err)
	}
	track, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || !track.Downloaded || len(track.DownloadVersions) != 1 {
		t.Fatalf("failed delete changed source state: %+v, %v", track.DownloadVersions, err)
	}
	started := make(chan struct{})
	r, w := io.Pipe()
	defer w.Close()
	type result struct {
		body io.ReadCloser
		err  error
	}
	done := make(chan result, 1)
	go func() {
		body, err := m.Open(ctx, scope, desc, p, func(context.Context) (pluginproto.OnlineAudio, io.ReadCloser, error) {
			close(started)
			return desc, r, nil
		})
		done <- result{body, err}
	}()
	<-started
	if err = m.Delete(ctx, scope.Owner, scope.Source, id); err != nil {
		t.Fatal(err)
	}
	if _, err = w.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	w.Close()
	inflight := <-done
	if inflight.err != nil {
		t.Fatal(inflight.err)
	}
	data, err := io.ReadAll(inflight.body)
	inflight.body.Close()
	if err != nil || string(data) != "abc" {
		t.Fatal("in-flight playback lost its bytes", err)
	}
	track, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || track.Downloaded {
		t.Fatal("previously started download restored deleted version", err)
	}
	if _, err = f.store.DB.ExecContext(ctx, "CREATE TEMP TRIGGER fail_cache_save BEFORE INSERT ON online_downloads BEGIN SELECT RAISE(ABORT,'injected insert failure'); END"); err != nil {
		t.Fatal(err)
	}
	if body, err := read(); err == nil {
		body.Close()
		t.Fatal("failed cache publication reported success")
	}
	if _, err = f.store.DB.ExecContext(ctx, "DROP TRIGGER fail_cache_save"); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, id+".audio")); err != nil {
		t.Fatal("failed publication removed the active reader's file", err)
	}
	track, err = f.store.FindTrack(ctx, "admin", f.track.ID)
	if err != nil || track.Downloaded {
		t.Fatal("failed publication restored a deleted version", err)
	}
	fresh, err := read()
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Close()
	data, err = io.ReadAll(old)
	old.Close()
	if err != nil || string(data) != "abc" {
		t.Fatal("deletion interrupted existing reader", err)
	}
	if _, err = os.Stat(filepath.Join(root, id+".audio")); err != nil {
		t.Fatal("previous reader removed the new version", err)
	}
	if err = m.Delete(ctx, scope.Owner, scope.Source, id); err != nil {
		t.Fatal(err)
	}
	fresh.Close()
	if _, err = os.Stat(filepath.Join(root, id+".audio")); !os.IsNotExist(err) {
		t.Fatalf("deleted file remains after last reader: %v", err)
	}
}
