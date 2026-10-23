package rpcbackend

import (
	"context"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/src/library"
)

func (c *Client) PublishOnlineTrack(ctx context.Context, owner, provider string, metadata online.Track) (result library.Track, err error) {
	err = c.Invoke(ctx, "PublishOnlineTrack", []any{owner, provider, metadata}, []any{&result})
	return
}
