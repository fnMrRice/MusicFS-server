package online

import (
	"context"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func (c *Client) checkApple(ctx context.Context, secret *accountSecret) error {
	b, _, err := c.accountHTTP(ctx, "GET", "https://api.music.apple.com/v1/me/storefront", "", map[string]string{"Authorization": "Bearer " + secret.DeveloperToken, "Music-User-Token": secret.UserToken, "Accept": "application/json"}, nil)
	if err != nil {
		return err
	}
	var result struct {
		Data []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"data"`
	}
	if err = decodeAccount(b, &result); err != nil {
		return err
	}
	if len(result.Data) != 1 || result.Data[0].Type != "storefronts" || len(result.Data[0].ID) != 2 {
		return upstreamInvalid()
	}
	// Apple does not disclose an Apple ID through this API. Use an opaque local
	// binding identity; never mislabel the storefront as an account identifier.
	if secret.AccountID == "" {
		secret.AccountID = domain.NewID()
	}
	secret.Name = "Apple Music (" + result.Data[0].ID + ")"
	return nil
}
