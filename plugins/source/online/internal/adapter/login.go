package adapter

import "context"

func (c *Client) AccountOptions(ctx context.Context) (AccountOptions, error) {
	if c.options.LoginOptions != nil {
		return c.options.LoginOptions(ctx)
	}
	return AccountOptions{Method: "qr", Configured: true, Validation: "liveAccountPending"}, ctx.Err()
}
