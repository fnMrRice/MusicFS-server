package adapter

import (
	"context"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/session"
)

func (c *Client) services() session.Runtime {
	return session.Runtime{
		Pending: &c.account.pending, Secret: &c.account.secret, View: &c.account.view, Key: &c.account.key,
		HTTP: c.accountHTTP, Complete: c.completeAccount, Admit: c.admit, BusinessError: c.businessError,
		Get: c.get, Require: c.requireNetease, ReadCursor: c.readCursor,
		ReadCursorToken: c.readCursorToken, WriteCursor: c.writeCursor,
		WriteVersionCursor: c.writeVersionCursor, WaitForMetadata: c.waitForMetadata,
	}
}

func emptySearchPage(kind string) SearchPage {
	return SearchPage{Kind: kind, Tracks: []Track{}, Albums: []Album{}, Artists: []Artist{}, Playlists: []Playlist{}, Consistency: Unversioned, ObservedAt: time.Now().UTC()}
}

func (c *Client) waitForMetadata(ctx context.Context) error {
	c.mu.Lock()
	wait := time.Until(c.next)
	c.mu.Unlock()
	if wait <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
