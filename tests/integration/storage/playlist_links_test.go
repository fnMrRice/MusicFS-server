package storage_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/library/playlists"
)

type playlistWriter struct {
	binding playlists.Binding
	sent    chan string
}

func (w playlistWriter) AuthorizePlaylistWrite(_ context.Context, _ string, ref playlists.RemoteRef) (playlists.Binding, error) {
	if ref != w.binding.RemoteRef {
		return playlists.Binding{}, domain.Fail("ACCESS_DENIED", "Account mismatch")
	}
	return w.binding, nil
}
func (w playlistWriter) AddPlaylistTrack(_ context.Context, owner string, ref playlists.RemoteRef, track string) error {
	if ref != w.binding.RemoteRef || owner != "admin" {
		return domain.Fail("ACCESS_DENIED", "Account mismatch")
	}
	w.sent <- track
	return nil
}

func TestPlaylistLinkScopesUniquenessAndUnlink(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	b := remoteBinding()
	b.AccountID = "account"
	first := playlistCreate(t, s, "admin", "First")
	second := playlistCreate(t, s, "admin", "Second")
	service := playlists.NewLinks(s, playlistWriter{binding: b})
	for range 2 {
		result, err := service.Link(ctx, "admin", b.RemoteRef, first.ID)
		if err != nil || result.PlaylistID != first.ID || result.PendingCount != 0 {
			t.Fatalf("idempotent bind: %+v %v", result, err)
		}
	}
	_, err := service.Link(ctx, "admin", b.RemoteRef, second.ID)
	matchingError(t, err, "CONFLICT")
	duplicate := b
	duplicate.InstanceID = "other-instance"
	_, err = s.LinkPlaylist(ctx, "admin", duplicate, second.ID)
	matchingError(t, err, "CONFLICT")
	_, err = service.Link(ctx, "other", b.RemoteRef, first.ID)
	matchingError(t, err, "NOT_FOUND")
	other := playlistCreate(t, s, "other", "Other account playlist")
	blocked, err := service.Link(ctx, "other", b.RemoteRef, other.ID)
	matchingError(t, err, "CONFLICT")
	if blocked != (playlists.PlaylistLink{}) || err.Error() != domain.Fail("CONFLICT", "Remote playlist already has a link").Error() {
		t.Fatalf("conflict disclosed another owner's link: %+v %v", blocked, err)
	}
	foreign, err := service.List(ctx, "other")
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign links: %v %v", foreign, err)
	}
	wrongAccount := b.RemoteRef
	wrongAccount.AccountID = "another-account"
	_, err = service.Link(ctx, "admin", wrongAccount, first.ID)
	matchingError(t, err, "ACCESS_DENIED")
	matchingError(t, service.Unlink(ctx, "admin", b.RemoteRef, second.ID), "CONFLICT")
	if err = service.Unlink(ctx, "admin", b.RemoteRef, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Link(ctx, "other", b.RemoteRef, other.ID); err != nil {
		t.Fatal(err)
	}
}

func TestPlaylistLinkOnlySendsNewNeteaseSources(t *testing.T) {
	s, _, locals := matchingFixture(t, 1)
	ctx := t.Context()
	b := remoteBinding()
	b.AccountID = "account"
	remoteInstance := "second-netease"
	if err := s.BindMusicSourcePlugins(ctx, map[string]string{
		b.InstanceID: "musicfs.plugin.source.netease", remoteInstance: "musicfs.plugin.source.netease",
	}); err != nil {
		t.Fatal(err)
	}
	add := func(id string) string {
		track, err := s.PublishOnlineTrack(ctx, "admin", remoteInstance, online.Track{ExternalID: id, Title: id})
		if err != nil {
			t.Fatal(err)
		}
		return track.ID
	}
	existing, automatic, forced, source := add("100"), add("200"), add("300"), add("400")
	p := playlistCreate(t, s, "admin", "Mixed", localBinding(existing))
	if _, err := s.LinkPlaylist(ctx, "admin", b, p.ID); err != nil {
		t.Fatal(err)
	}
	remoteSources, err := s.EnsurePlaylistTrackSources(ctx, "admin", b.InstanceID, b.AccountID, []string{"500"})
	if err != nil {
		t.Fatal(err)
	}
	newEntries := localBinding(source, locals[0].ID)
	for _, ref := range []playlists.SourceRef{
		{TrackID: &automatic}, {TrackID: &forced, ForcedSourceID: &forced},
		{RemoteSourceID: &remoteSources[0].ID}, {SourceTrackID: &source},
	} {
		newEntries.Entries = append(newEntries.Entries, playlists.LocalEntry{Source: ref})
	}
	input := playlists.EditPlaylistRequest{ExpectedRevision: p.RuleRevision, Name: p.Name, Bindings: append(p.Bindings, newEntries)}
	updated, err := s.EditPlaylist(ctx, "admin", p.ID, input, "linked-edit")
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.PlaylistLinks(ctx, "admin")
	if err != nil || len(items) != 1 || items[0].PendingCount != 4 {
		t.Fatalf("queued tracks: %+v %v", items, err)
	}
	// 首个待发送任务在失败后保留，可恢复进度并向关联页提供错误原因。
	next, err := s.NextPlaylistAddition(ctx)
	if err != nil || next == nil || next.TrackID != "400" {
		t.Fatalf("pending addition: %v %v", next, err)
	}
	if err = s.FinishPlaylistAddition(ctx, next.ID, "RATE_LIMITED", time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	items, err = s.PlaylistLinks(ctx, "admin")
	if err != nil || items[0].LastErrorCode != "RATE_LIMITED" || items[0].PendingCount != 4 {
		t.Fatal(items, err)
	}
	writer := playlistWriter{binding: b, sent: make(chan string, 10)}
	stop := playlists.NewLinks(s, writer).Start(ctx)
	var delivered []string
	for range 3 {
		select {
		case id := <-writer.sent:
			delivered = append(delivered, id)
		case <-time.After(5 * time.Second):
			stop()
			t.Fatal("timed out waiting for linked playlist delivery")
		}
	}
	// 等待接收回执提交，避免把已发送但尚未确认的最后一首视为完成。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		items, err = s.PlaylistLinks(ctx, "admin")
		if err != nil || items[0].PendingCount == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	if err != nil || items[0].PendingCount != 1 || !reflect.DeepEqual(delivered, []string{"200", "300", "500"}) {
		t.Fatalf("delivered: %v status: %v err: %v", delivered, items, err)
	}
	if _, err = s.EditPlaylist(ctx, "admin", p.ID, input, "linked-edit"); err != nil {
		t.Fatal(err)
	}
	_, err = s.EditPlaylist(ctx, "admin", p.ID, playlists.EditPlaylistRequest{ExpectedRevision: updated.RuleRevision, Name: "Renamed", Bindings: updated.Bindings}, "linked-rename")
	if err != nil {
		t.Fatal(err)
	}
	next, err = s.NextPlaylistAddition(ctx)
	if err != nil || next != nil {
		t.Fatalf("replay or rename queued more additions: %v %v", next, err)
	}
	if err = s.UnlinkPlaylist(ctx, "admin", b.RemoteRef, p.ID); err != nil {
		t.Fatal(err)
	}
	_, err = s.LinkPlaylist(ctx, "admin", b, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	items, err = s.PlaylistLinks(ctx, "admin")
	if err != nil || items[0].PendingCount != 0 || items[0].LastErrorCode != "" {
		t.Fatal(items, err)
	}
}

func TestPlaylistLinkBatchSelectionAndReplay(t *testing.T) {
	s, _, _ := matchingFixture(t, 2)
	ctx := t.Context()
	b := remoteBinding()
	b.AccountID = "account"
	for _, id := range []string{"50", "60"} {
		if _, err := s.PublishOnlineTrack(ctx, "admin", b.InstanceID, online.Track{ExternalID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	p := playlistCreate(t, s, "admin", "Batch")
	if _, err := s.LinkPlaylist(ctx, "admin", b, p.ID); err != nil {
		t.Fatal(err)
	}
	request := playlists.AppendSelectionRequest{ExpectedRevision: 1, Selection: actionSelection(t, s)}
	for range 2 {
		result, err := s.AppendPlaylistSelection(ctx, "admin", p.ID, request, "link-batch")
		if err != nil || result.Count != 4 {
			t.Fatal(result, err)
		}
	}
	items, err := s.PlaylistLinks(ctx, "admin")
	if err != nil || len(items) != 1 || items[0].PendingCount != 2 {
		t.Fatal(items, err)
	}
}

type blockedPlaylistWriter struct {
	playlistWriter
	started  chan struct{}
	finished chan struct{}
}

func (w blockedPlaylistWriter) AddPlaylistTrack(ctx context.Context, _ string, _ playlists.RemoteRef, _ string) error {
	close(w.started)
	<-ctx.Done()
	close(w.finished)
	return ctx.Err()
}

func TestPlaylistUnlinkWaitsForInFlightAddition(t *testing.T) {
	s := evidenceStore(t)
	ctx := t.Context()
	b := remoteBinding()
	b.AccountID = "account"
	writer := blockedPlaylistWriter{playlistWriter: playlistWriter{binding: b}, started: make(chan struct{}), finished: make(chan struct{})}
	service := playlists.NewLinks(s, writer)
	p := playlistCreate(t, s, "admin", "Linked")
	if _, err := service.Link(ctx, "admin", b.RemoteRef, p.ID); err != nil {
		t.Fatal(err)
	}
	track, err := s.PublishOnlineTrack(ctx, "admin", b.InstanceID, online.Track{ExternalID: "50", Title: "Fifty"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.EditPlaylist(ctx, "admin", p.ID, playlists.EditPlaylistRequest{ExpectedRevision: 1, Name: p.Name, Bindings: []playlists.PlaylistBinding{localBinding(track.ID)}}, "enqueue-in-flight")
	if err != nil {
		t.Fatal(err)
	}
	stop := service.Start(ctx)
	defer stop()
	select {
	case <-writer.started:
	case <-time.After(5 * time.Second):
		t.Fatal("write did not start")
	}
	if err := service.Unlink(ctx, "admin", b.RemoteRef, p.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-writer.finished:
	default:
		t.Fatal("unlink returned before the in-flight write finished")
	}
	next, err := s.NextPlaylistAddition(ctx)
	if err != nil || next != nil {
		t.Fatal(next, err)
	}
}
