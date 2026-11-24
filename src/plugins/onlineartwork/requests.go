package onlineartwork

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

type pendingArtwork struct {
	done    chan struct{}
	cancel  context.CancelFunc
	readers int
	id      string
	err     error
}

// 同图只启动一次下载。满队列等待空位；最后一个读者离开时中止对应的后台工作。
func (s *Service) waitArtwork(ctx context.Context, owner string, track library.Track) (string, error) {
	key := [2]string{owner, track.ID}
	var p *pendingArtwork
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		s.mu.Lock()
		p = s.requests[key]
		if p != nil && p.readers > 0 {
			p.readers++
			s.mu.Unlock()
			break
		}
		if p == nil && len(s.requests) < 2*online.MaxPageItems {
			work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
			p = &pendingArtwork{done: make(chan struct{}), cancel: cancel, readers: 1}
			s.requests[key] = p
			go s.fetch(work, key, p, track)
			s.mu.Unlock()
			break
		}
		changed := s.changed
		if p != nil {
			changed = p.done
		}
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-changed:
		}
	}
	defer func() {
		s.mu.Lock()
		p.readers--
		if p.readers == 0 {
			p.cancel()
		}
		s.mu.Unlock()
	}()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-p.done:
		return p.id, p.err
	}
}

func (s *Service) fetch(ctx context.Context, key [2]string, p *pendingArtwork, track library.Track) {
	defer p.cancel()
	p.id, p.err = s.objectID(ctx, key[0], track)
	s.mu.Lock()
	delete(s.requests, key)
	close(p.done)
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}
