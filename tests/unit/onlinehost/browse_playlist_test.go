package onlinehost_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type playlistDetailProvider struct {
	observedPlaylist
	account                   string
	extraMember               string
	observeCalls, metaCalls   atomic.Int32
	fail                      bool
	started, resume, sessions chan struct{}
}

func (p *playlistDetailProvider) PlatformSession(context.Context) (online.AccountView, error) {
	if p.sessions != nil {
		select {
		case p.sessions <- struct{}{}:
		default:
		}
	}
	if p.account == "" {
		return online.AccountView{State: "signedOut"}, nil
	}
	return online.AccountView{State: "signedIn", AccountID: p.account, Revision: "session-" + p.account}, nil
}

func (p *playlistDetailProvider) ObservePlaylist(ctx context.Context, id string) (online.PlaylistObservation, error) {
	n := p.observeCalls.Add(1)
	if p.started != nil && n == 1 {
		close(p.started)
		select {
		case <-ctx.Done():
			return online.PlaylistObservation{}, ctx.Err()
		case <-p.resume:
		}
	}
	if p.fail {
		return online.PlaylistObservation{}, domain.Fail("TEMPORARY_FAILURE", "Fixture playlist unavailable")
	}
	count := int64(3)
	playlist := online.Playlist{ExternalID: id, Title: fmt.Sprintf("%s-%d", p.account, n), TrackCount: &count, SourceURL: "https://music.163.com/playlist?id=" + id}
	entries := []online.PlaylistEntry{{Position: 0, ExternalTrackID: "41"}, {Position: 1, ExternalTrackID: "42"}, {Position: 2, ExternalTrackID: "41"}}
	if p.extraMember != "" {
		entries = append(entries, online.PlaylistEntry{Position: len(entries), ExternalTrackID: p.extraMember})
		count++
	}
	return online.PlaylistObservation{Playlist: playlist, Entries: entries, Complete: true, Consistency: online.Unversioned,
		MetadataDigest: online.PlaylistDigest(playlist, entries), ObservedAt: time.Now().UTC()}, nil
}

func (p *playlistDetailProvider) Playlists(context.Context, online.PlaylistsRequest) (online.PlaylistsPage, error) {
	count := int64(3)
	if p.extraMember != "" {
		count++
	}
	return online.PlaylistsPage{Consistency: online.Unversioned, ObservedAt: time.Now().UTC(),
		Items: []online.Playlist{{ExternalID: "91", Title: "Favorites", TrackCount: &count}}}, nil
}

func TestManualSyncReadsNewPlaylistMembersAndRefreshesBrowsePages(t *testing.T) {
	ctx := t.Context()
	provider := &playlistDetailProvider{account: "123"}
	host := detailHost(t, t.TempDir(), provider)
	directory := func(want int64) {
		t.Helper()
		page, err := host.BrowsePlaylists(ctx, "owner", "net", online.PlaylistsRequest{Limit: 10}, false)
		if err != nil || len(page.Items) != 1 || page.Items[0].TrackCount == nil || *page.Items[0].TrackCount != want {
			t.Fatalf("unexpected playlist count: %+v, %v", page, err)
		}
	}
	directory(3)
	request := online.PlaylistTracksRequest{ExternalID: "91", Limit: 2}
	page, err := host.BrowsePlaylistTracks(ctx, "owner", "net", request)
	if err != nil || page.NextCursor == nil {
		t.Fatalf("missing page of initial tracks: %+v, %v", page, err)
	}
	provider.extraMember = "43"
	r := host.PlaylistRemote()
	b, err := r.Authorize(ctx, "owner", playlists.RemoteRef{InstanceID: "net", AccountID: "123", ExternalID: "91"})
	if err != nil {
		t.Fatal(err)
	}
	o, err := r.ObservePlaylist(ctx, "owner", b)
	if err != nil || len(o.Entries) != 4 || len(o.Tracks) != 3 || o.Tracks[2].ExternalID != "43" {
		t.Fatalf("sync missed new member: %+v, %v", o, err)
	}
	directory(4)
	request.Cursor = *page.NextCursor
	_, err = host.BrowsePlaylistTracks(ctx, "owner", "net", request)
	requireDirectoryError(t, err, "CURSOR_EXPIRED")
	page, err = host.BrowsePlaylistTracks(ctx, "owner", "net", online.PlaylistTracksRequest{ExternalID: "91", Limit: 10})
	if err != nil || len(page.Items) != 4 || page.Items[3].ExternalID != "43" {
		t.Fatalf("preview still uses the old song list: %+v, %v", page, err)
	}
}

func (p *playlistDetailProvider) MetadataBatch(_ context.Context, ids []string) (online.TrackBatch, error) {
	p.metaCalls.Add(1)
	batch := online.TrackBatch{}
	for _, id := range ids {
		track := online.Track{ExternalID: id, SourceURL: "https://music.163.com/song?id=" + id, Title: "Song " + id, Artists: []string{}, PlaybackAvailability: "unknown"}
		track.MetadataDigest = online.Digest(track)
		batch.Items = append(batch.Items, online.TrackLookup{ExternalID: id, Track: &track})
	}
	return batch, nil
}

func (p *playlistDetailProvider) PlaylistTracks(context.Context, online.PlaylistTracksRequest) (online.PlaylistTracksPage, error) {
	return online.PlaylistTracksPage{}, errors.New("unexpected live playlist page")
}

func detailHost(t *testing.T, root string, provider *playlistDetailProvider) *onlinehost.Service {
	t.Helper()
	host, err := onlinehost.New([]onlinehost.Runtime{{Owner: "owner", InstanceID: "net", ConfigRevision: "r1",
		Enabled: true, Platform: online.Netease, Provider: provider}}, onlinehost.WithPlaylistCache(root))
	if err != nil {
		t.Fatal(err)
	}
	return host
}

func TestBrowsePlaylistDailyObservationAndTrackPagesSurviveRestart(t *testing.T) {
	ctx, root := t.Context(), t.TempDir()
	p := &playlistDetailProvider{account: "123"}
	host := detailHost(t, root, p)
	first, err := host.BrowsePlaylist(ctx, "owner", "net", "91", false)
	if err != nil || len(first.Entries) != 3 {
		t.Fatal("initial observation", first, err)
	}
	again, err := host.BrowsePlaylist(ctx, "owner", "net", "91", false)
	if err != nil || !again.ObservedAt.Equal(first.ObservedAt) || p.observeCalls.Load() != 1 {
		t.Fatal("same-day observation was not reused", again, err)
	}
	pageRequest := online.PlaylistTracksRequest{ExternalID: "91", Limit: 2}
	page, err := host.BrowsePlaylistTracks(ctx, "owner", "net", pageRequest)
	if err != nil || len(page.Items) != 2 || page.Items[0].Track == nil || page.Items[0].Track.ExternalID != "41" ||
		page.Items[1].Position != 1 || page.Items[1].ExternalID != "42" || page.NextCursor == nil {
		t.Fatal("first track page", page, err)
	}
	nextRequest := pageRequest
	nextRequest.Cursor = *page.NextCursor
	tail, err := host.BrowsePlaylistTracks(ctx, "owner", "net", nextRequest)
	if err != nil || len(tail.Items) != 1 || tail.Items[0].Position != 2 || tail.Items[0].ExternalID != "41" ||
		tail.NextCursor != nil || p.observeCalls.Load() != 1 || p.metaCalls.Load() == 0 {
		t.Fatal("second track page lost the repeated song or refetched the observation", tail, err)
	}
	restarted := &playlistDetailProvider{account: "123", fail: true}
	host = detailHost(t, root, restarted)
	for _, req := range []online.PlaylistTracksRequest{pageRequest, nextRequest, {ExternalID: "91", Limit: 1}} {
		cached, err := host.BrowsePlaylistTracks(ctx, "owner", "net", req)
		if err != nil || len(cached.Items) == 0 || cached.Items[0].Track == nil || cached.MetadataDigest != first.MetadataDigest ||
			restarted.observeCalls.Load() != 0 || restarted.metaCalls.Load() != 0 {
			t.Fatal("restarted host did not reuse track metadata", cached, err)
		}
	}
	restarted.fail = false
	if _, err := host.BrowsePlaylist(ctx, "owner", "net", "91", true); err != nil {
		t.Fatal(err)
	}
	_, err = host.BrowsePlaylistTracks(ctx, "owner", "net", nextRequest)
	requireDirectoryError(t, err, "CURSOR_EXPIRED")
}

func TestBrowsePlaylistRetryRefreshAndAccountIsolation(t *testing.T) {
	ctx, root := t.Context(), t.TempDir()
	p := &playlistDetailProvider{account: "123", fail: true}
	host := detailHost(t, root, p)
	open := func(force bool) (online.PlaylistObservation, error) {
		return host.BrowsePlaylist(ctx, "owner", "net", "91", force)
	}
	_, err := open(false)
	requireDirectoryError(t, err, "TEMPORARY_FAILURE")
	p.fail = false
	saved, err := open(false)
	if err != nil || p.observeCalls.Load() != 2 {
		t.Fatal("ordinary read did not retry an unsuccessful first visit", saved, err)
	}
	p.fail = true
	_, err = open(true)
	requireDirectoryError(t, err, "TEMPORARY_FAILURE")
	p.fail = false
	updated, err := open(true)
	if err != nil || updated.MetadataDigest == saved.MetadataDigest || p.observeCalls.Load() != 4 {
		t.Fatal("explicit refresh did not replace the observation after failure", updated, err)
	}
	_, err = host.BrowsePlaylist(ctx, "other", "net", "91", false)
	requireDirectoryError(t, err, "NOT_FOUND")
	p.account = ""
	_, err = open(false)
	requireDirectoryError(t, err, "PLATFORM_AUTH_REQUIRED")
	p.account = "456"
	other, err := open(false)
	if err != nil || other.Playlist.Title != "456-5" {
		t.Fatal("account switch exposed the prior account's observation", other, err)
	}
}

func TestBrowsePlaylistRechecksOnFirstVisitOfNewDay(t *testing.T) {
	ctx, root := t.Context(), t.TempDir()
	p := &playlistDetailProvider{account: "123"}
	host := detailHost(t, root, p)
	first, err := host.BrowsePlaylist(ctx, "owner", "net", "91", false)
	if err != nil {
		t.Fatal(err)
	}
	// 将已写入的当日快照回拨一天，避免测试依赖墙钟跨日。
	files := 0
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record map[string]json.RawMessage
		if json.Unmarshal(data, &record) != nil || record["day"] == nil {
			return nil
		}
		record["day"], _ = json.Marshal(time.Now().AddDate(0, 0, -1).Format(time.DateOnly))
		data, err = json.Marshal(record)
		if err != nil {
			return err
		}
		files++
		return os.WriteFile(path, data, 0600)
	})
	if err != nil || files == 0 {
		t.Fatal("could not age the persisted snapshot", err)
	}
	host = detailHost(t, root, p)
	fresh, err := host.BrowsePlaylist(ctx, "owner", "net", "91", false)
	if err != nil || fresh.MetadataDigest == first.MetadataDigest || p.observeCalls.Load() != 2 {
		t.Fatal("first visit of the new day kept the old observation", fresh, err)
	}
}

func TestBrowsePlaylistSharesConcurrentFirstVisit(t *testing.T) {
	p := &playlistDetailProvider{account: "123", started: make(chan struct{}), resume: make(chan struct{}), sessions: make(chan struct{}, 1)}
	host := detailHost(t, t.TempDir(), p)
	result := make(chan error, 2)
	open := func() { _, err := host.BrowsePlaylist(t.Context(), "owner", "net", "91", false); result <- err }
	go open()
	select {
	case <-p.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first upstream request did not start")
	}
	<-p.sessions
	go open()
	select {
	case <-p.sessions:
	case <-time.After(3 * time.Second):
		close(p.resume)
		t.Fatal("second reader did not enter the host while the first observation was pending")
	}
	close(p.resume)
	for range 2 {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	if p.observeCalls.Load() != 1 {
		t.Fatal("concurrent readers did not share the first upstream request")
	}
}
