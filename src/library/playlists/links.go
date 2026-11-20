package playlists

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

type PlaylistLink struct {
	RemoteRef
	Platform      string `json:"platform"`
	PlaylistID    string `json:"playlistId"`
	PendingCount  int    `json:"pendingCount"`
	LastErrorCode string `json:"lastErrorCode,omitempty"`
}

type LinkAddition struct {
	ID    int64
	Owner string
	RemoteRef
	TrackID  string
	Attempts int
}

type LinkRepository interface {
	PlaylistLinks(context.Context, string) ([]PlaylistLink, error)
	LinkPlaylist(context.Context, string, Binding, string) (PlaylistLink, error)
	UnlinkPlaylist(context.Context, string, RemoteRef, string) error
	NextPlaylistAddition(context.Context) (*LinkAddition, error)
	FinishPlaylistAddition(context.Context, int64, string, int64) error
}

type PlaylistWriter interface {
	AuthorizePlaylistWrite(context.Context, string, RemoteRef) (Binding, error)
	AddPlaylistTrack(context.Context, string, RemoteRef, string) error
}

type LinkService struct {
	repo   LinkRepository
	writer PlaylistWriter
	mu     sync.Mutex
	active *LinkAddition
	cancel context.CancelFunc
	done   chan struct{}
}

func NewLinks(repo LinkRepository, writer PlaylistWriter) *LinkService {
	return &LinkService{repo: repo, writer: writer}
}

func (s *LinkService) List(ctx context.Context, owner string) ([]PlaylistLink, error) {
	return s.repo.PlaylistLinks(ctx, owner)
}

func (s *LinkService) Link(ctx context.Context, owner string, ref RemoteRef, playlistID string) (PlaylistLink, error) {
	if ref.Validate() != nil || ref.AccountID == "" || !ValidID(playlistID) {
		return PlaylistLink{}, Invalid()
	}
	binding, err := s.writer.AuthorizePlaylistWrite(ctx, owner, ref)
	if err != nil {
		return PlaylistLink{}, err
	}
	if binding.RemoteRef != ref {
		return PlaylistLink{}, domain.Fail("ACCESS_DENIED", "Playlist write authorization does not match the requested platform account")
	}
	return s.repo.LinkPlaylist(ctx, owner, binding, playlistID)
}

func (s *LinkService) Unlink(ctx context.Context, owner string, ref RemoteRef, playlistID string) error {
	if ref.Validate() != nil || ref.AccountID == "" || !ValidID(playlistID) {
		return Invalid()
	}
	// 领取任务和解除关联共用短锁；只有目标相同的在途请求需要取消并等待结束。
	s.mu.Lock()
	err := s.repo.UnlinkPlaylist(ctx, owner, ref, playlistID)
	var done chan struct{}
	if err == nil && s.active != nil && s.active.Owner == owner && s.active.RemoteRef == ref {
		s.cancel()
		done = s.done
	}
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

func (s *LinkService) Start(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() { defer close(done); s.run(ctx) }()
	return func() { cancel(); <-done }
}

// 单工作协程按入队顺序处理到期任务。中断后保留记录，重复投递由平台添加操作去重。
func (s *LinkService) run(ctx context.Context) {
	for ctx.Err() == nil {
		s.mu.Lock()
		next, err := s.repo.NextPlaylistAddition(ctx)
		var send context.Context
		if err == nil && next != nil {
			send, s.cancel = context.WithTimeout(ctx, 20*time.Second)
			s.active, s.done = next, make(chan struct{})
		}
		s.mu.Unlock()
		if send != nil {
			err = s.writer.AddPlaylistTrack(send, next.Owner, next.RemoteRef, next.TrackID)
			s.mu.Lock()
			s.cancel()
			close(s.done)
			s.active = nil
			s.mu.Unlock()
			if ctx.Err() != nil {
				return
			}
			code := ""
			var retryAt int64
			if err != nil {
				code = FailureCode(err)
				delay := min(time.Hour, 5*time.Second<<min(10, next.Attempts))
				retryAt = time.Now().Add(delay).Unix()
				slog.WarnContext(ctx, "Playlist track sync deferred", "additionId", next.ID, "errorCode", code)
			}
			err = s.repo.FinishPlaylistAddition(ctx, next.ID, code, retryAt)
			if err == nil {
				continue
			}
		}
		if err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "Playlist sync queue unavailable", "errorCode", FailureCode(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}
