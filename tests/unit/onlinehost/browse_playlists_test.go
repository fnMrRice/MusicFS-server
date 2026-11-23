package onlinehost_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/plugins/onlinehost"
)

type directoryProvider struct {
	observedPlaylist
	requests []online.PlaylistsRequest
	failNext bool
	changed  bool
}

func requireDirectoryError(t *testing.T, err error, code string) {
	t.Helper()
	var failure *domain.Error
	if !errors.As(err, &failure) || failure.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func (p *directoryProvider) Playlists(_ context.Context, req online.PlaylistsRequest) (online.PlaylistsPage, error) {
	p.requests = append(p.requests, req)
	page := online.PlaylistsPage{Consistency: online.Unversioned, ObservedAt: time.Now().UTC()}
	ids := []string{"1", "2"}
	switch req.Cursor {
	case "":
		next := "plugin-page-two"
		page.NextCursor = &next
	case "plugin-page-two":
		if p.failNext {
			return online.PlaylistsPage{}, domain.Fail("TEMPORARY_FAILURE", "Upstream playlist read failed")
		}
		ids = []string{"3"}
		if p.changed {
			ids = []string{"4"}
		}
	default:
		return online.PlaylistsPage{}, fmt.Errorf("unexpected plugin cursor %q", req.Cursor)
	}
	for _, id := range ids {
		page.Items = append(page.Items, online.Playlist{ExternalID: id, Title: id, SourceURL: "https://music.163.com/playlist?id=" + id})
	}
	return page, nil
}

func directoryHost(t *testing.T, root string, p *directoryProvider) *onlinehost.Service {
	t.Helper()
	platform := p.platform
	if platform == "" {
		platform = online.Netease
	}
	runtimes := []onlinehost.Runtime{{Owner: "owner", InstanceID: "remote", Platform: platform, ConfigRevision: "r1", Enabled: true, Provider: p}}
	service, err := onlinehost.New(runtimes, onlinehost.WithPlaylistCache(root))
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestAccountPlaylistDirectoryPagesSurviveWorkerCursorExpiryAndRestart(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	provider := &directoryProvider{}
	host := directoryHost(t, root, provider)
	first, err := host.BrowsePlaylists(ctx, "owner", "remote", online.PlaylistsRequest{Limit: 2}, false)
	if err != nil || len(first.Items) != 2 || first.Items[1].ExternalID != "2" || first.NextCursor == nil {
		t.Fatal("invalid first playlist page", first, err)
	}
	want := []online.PlaylistsRequest{{Limit: online.MaxPageItems}, {Limit: online.MaxPageItems, Cursor: "plugin-page-two"}}
	if !reflect.DeepEqual(provider.requests, want) {
		t.Fatal("first page did not fetch the full directory", provider.requests)
	}
	// 更换 Worker 后原插件游标已经不可用，宿主继续读取同一份磁盘目录。
	restarted := &directoryProvider{failNext: true}
	host = directoryHost(t, root, restarted)
	next := online.PlaylistsRequest{Limit: 2, Cursor: *first.NextCursor}
	last, err := host.BrowsePlaylists(ctx, "owner", "remote", next, false)
	if err != nil || len(last.Items) != 1 || last.Items[0].ExternalID != "3" || last.NextCursor != nil ||
		!last.ObservedAt.Equal(first.ObservedAt) || len(restarted.requests) != 0 {
		t.Fatal("cached page depended on the worker cursor", last, err, restarted.requests)
	}
	restarted.failNext, restarted.changed = false, true
	updated, err := host.BrowsePlaylists(ctx, "owner", "remote", online.PlaylistsRequest{Limit: 4}, true)
	if err != nil || len(updated.Items) != 3 || updated.Items[2].ExternalID != "4" {
		t.Fatal("refresh did not publish the new directory", updated, err)
	}
	_, err = host.BrowsePlaylists(ctx, "owner", "remote", next, false)
	requireDirectoryError(t, err, "CURSOR_EXPIRED")
}

func TestAccountPlaylistDirectoryRetriesAllPagesAfterFailure(t *testing.T) {
	ctx := context.Background()
	p := &directoryProvider{failNext: true}
	host := directoryHost(t, t.TempDir(), p)
	req := online.PlaylistsRequest{Limit: 2}
	page, err := host.BrowsePlaylists(ctx, "owner", "remote", req, false)
	requireDirectoryError(t, err, "TEMPORARY_FAILURE")
	if len(page.Items) != 0 {
		t.Fatal("published a partial directory", page)
	}
	p.failNext = false
	if _, err := host.BrowsePlaylists(ctx, "owner", "remote", req, false); err != nil || len(p.requests) != 4 || p.requests[2].Cursor != "" {
		t.Fatal("failed directory did not restart at the first page", p.requests, err)
	}
}

func TestOtherProvidersKeepTheirPlaylistCursor(t *testing.T) {
	p := &directoryProvider{observedPlaylist: observedPlaylist{platform: online.QQMusic}}
	host := directoryHost(t, t.TempDir(), p)
	req := online.PlaylistsRequest{Limit: 2}
	page, err := host.BrowsePlaylists(context.Background(), "owner", "remote", req, true)
	if err != nil || page.NextCursor == nil || *page.NextCursor != "plugin-page-two" || !reflect.DeepEqual(p.requests, []online.PlaylistsRequest{req}) {
		t.Fatal("third-party pagination changed", page, p.requests, err)
	}
}
