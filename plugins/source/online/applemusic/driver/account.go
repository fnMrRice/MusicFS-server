package driver

import (
	"context"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/session"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

type Client struct {
	session.Unsupported
	session.Runtime
}

func New(runtime session.Runtime) session.Driver { return &Client{Runtime: runtime} }

func (c *Client) Begin(_ context.Context, request online.AccountRequest) error {
	if !session.ValidToken(request.DeveloperToken) {
		return domain.Fail("AUTH_REQUIRED", "Apple Music developer configuration is required")
	}
	c.Pending.DeveloperToken = request.DeveloperToken
	c.View.Method = "musicKit"
	return nil
}

func (c *Client) Poll(ctx context.Context, request online.AccountRequest) error {
	if request.Action != "complete" || !session.ValidToken(request.UserToken) {
		return domain.Fail("INVALID_ARGUMENT", "MusicKit user authorization is required")
	}
	c.Pending.UserToken = request.UserToken
	if err := c.Check(ctx, c.Pending); err != nil {
		return err
	}
	c.Complete()
	return nil
}

func (c *Client) Check(ctx context.Context, secret *session.Secret) error {
	b, _, err := c.HTTP(ctx, "GET", "https://api.music.apple.com/v1/me/storefront", "", map[string]string{"Authorization": "Bearer " + secret.DeveloperToken, "Music-User-Token": secret.UserToken, "Accept": "application/json"}, nil)
	if err != nil {
		return err
	}
	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err = json.Unmarshal(b, &result); err != nil {
		return online.UpstreamInvalid()
	}
	if len(result.Data) != 1 || result.Data[0].Type != "storefronts" || len(result.Data[0].ID) != 2 {
		return online.UpstreamInvalid()
	}
	// Apple does not disclose an Apple ID through this API. Use an opaque local
	// binding identity; never mislabel the storefront as an account identifier.
	if secret.AccountID == "" {
		secret.AccountID = domain.NewID()
	}
	secret.Name = "Apple Music (" + result.Data[0].ID + ")"
	return nil
}
