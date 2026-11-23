package adapter

import (
	"context"
	"errors"

	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

type accountContextKey struct{}

// The account lock covers the entire operation, including multi-request pages.
// A logout signal cancels the transport before waiting for this lock.
func authorized[T any](ctx context.Context, c *Client, run func(context.Context) (T, error)) (T, error) {
	var empty T
	if ctx.Value(accountContextKey{}) == c {
		return run(ctx)
	}
	a := &c.account
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if a.view.State != "signedIn" || a.secret.AccountID == "" || a.revoked.Load() {
		return empty, platformLoginRequired()
	}
	ctx, cancel := context.WithCancel(ctx)
	a.active.Store(&cancel)
	defer func() { a.active.Store(nil); cancel() }()
	if a.revoked.Load() {
		return empty, platformLoginRequired()
	}
	// 恢复时的校验也占用一次请求间隔，让紧随其后的业务请求等到可用。
	if a.waitAfterRestore {
		if err := c.waitForMetadata(ctx); err != nil {
			return empty, err
		}
		a.waitAfterRestore = false
	}
	value, err := run(context.WithValue(ctx, accountContextKey{}, c))
	if a.revoked.Load() {
		return empty, platformLoginRequired()
	}
	var failure *domain.Error
	if errors.As(err, &failure) && failure.Code == "AUTH_REQUIRED" {
		a.secret = accountSecret{}
		a.view = AccountView{State: "expired"}
		return empty, platformLoginRequired()
	}
	if err != nil {
		return empty, err
	}
	if err = ctx.Err(); err != nil {
		return empty, err
	}
	return value, nil
}

func platformLoginRequired() error {
	return domain.Fail("PLATFORM_AUTH_REQUIRED", "Sign in to the music platform before using this source")
}

func (c *Client) Search(ctx context.Context, r SearchRequest) (SearchPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (SearchPage, error) { return c.search(ctx, r) })
}
func (c *Client) Metadata(ctx context.Context, id string) (Track, error) {
	return authorized(ctx, c, func(ctx context.Context) (Track, error) { return c.driver.Metadata(ctx, id) })
}
func (c *Client) TrackDetails(ctx context.Context, id string) (pluginproto.TrackDetails, error) {
	return authorized(ctx, c, func(ctx context.Context) (pluginproto.TrackDetails, error) {
		return c.driver.TrackDetails(ctx, id)
	})
}
func (c *Client) MetadataBatch(ctx context.Context, ids []string) (TrackBatch, error) {
	return authorized(ctx, c, func(ctx context.Context) (TrackBatch, error) { return c.driver.MetadataBatch(ctx, ids) })
}
func (c *Client) ObservePlaylist(ctx context.Context, id string) (PlaylistObservation, error) {
	return authorized(ctx, c, func(ctx context.Context) (PlaylistObservation, error) { return c.driver.ObservePlaylist(ctx, id) })
}
func (c *Client) PlaylistTracks(ctx context.Context, r PlaylistTracksRequest) (PlaylistTracksPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (PlaylistTracksPage, error) { return c.driver.PlaylistTracks(ctx, r) })
}
func (c *Client) PublicRecommendations(ctx context.Context, n int) (RecommendationPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (RecommendationPage, error) {
		return c.driver.PublicRecommendations(ctx, n)
	})
}
func (c *Client) DailySongs(ctx context.Context, limit int) (online.DailySongsPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (online.DailySongsPage, error) {
		return c.driver.DailySongs(ctx, limit)
	})
}
func (c *Client) Album(ctx context.Context, id string) (Album, error) {
	return authorized(ctx, c, func(ctx context.Context) (Album, error) { return c.driver.Album(ctx, id) })
}
func (c *Client) Artist(ctx context.Context, id string) (Artist, error) {
	return authorized(ctx, c, func(ctx context.Context) (Artist, error) { return c.driver.Artist(ctx, id) })
}
func (c *Client) AlbumTracks(ctx context.Context, r EntityPageRequest) (SearchPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (SearchPage, error) { return c.driver.AlbumTracks(ctx, r) })
}
func (c *Client) ArtistAlbums(ctx context.Context, r EntityPageRequest) (SearchPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (SearchPage, error) { return c.driver.ArtistAlbums(ctx, r) })
}
func (c *Client) ArtistTracks(ctx context.Context, r EntityPageRequest) (SearchPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (SearchPage, error) { return c.driver.ArtistTracks(ctx, r) })
}
func (c *Client) Resolve(ctx context.Context, id string) error {
	_, err := authorized(ctx, c, func(ctx context.Context) (struct{}, error) { return struct{}{}, c.resolve(ctx, id) })
	return err
}

// PlatformSession reports local validation state without initiating network login.
func (c *Client) PlatformSession(ctx context.Context) (AccountView, error) {
	result, err := c.Account(ctx, AccountRequest{Action: "state"})
	return result.View, err
}
func (c *Client) AccountScope(ctx context.Context) (string, string, error) {
	account, revision, _, err := c.RecommendationAccountScope(ctx)
	return account, revision, err
}
func (c *Client) RecommendationAccountScope(ctx context.Context) (string, string, string, error) {
	result, err := c.Account(ctx, AccountRequest{Action: "state"})
	if err != nil {
		return "", "", "", err
	}
	return result.RecommendationScope()
}

// Playlists keeps account ownership and cursor binding in the shared runtime.
func (c *Client) Playlists(ctx context.Context, request PlaylistsRequest) (PlaylistsPage, error) {
	return authorized(ctx, c, func(ctx context.Context) (PlaylistsPage, error) {
		return c.driver.Playlists(ctx, request)
	})
}

func (c *Client) AddPlaylistTrack(ctx context.Context, request online.PlaylistTrackAddition) error {
	_, err := authorized(ctx, c, func(ctx context.Context) (struct{}, error) {
		if request.AccountID == "" || request.AccountID != c.account.secret.AccountID {
			return struct{}{}, domain.Fail("ACCESS_DENIED", "Platform account does not match the linked playlist")
		}
		return struct{}{}, c.driver.AddPlaylistTrack(ctx, request)
	})
	return err
}
