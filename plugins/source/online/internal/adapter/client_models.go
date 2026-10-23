package adapter

import (
	"context"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
	"net/http"
	"time"
)

// Options are trusted composition inputs, not fields accepted from a client.
// Transport is a test/host injection point; nil uses fixed-host, public-IP TLS.
type Options struct {
	Transport http.RoundTripper
	Timeout   time.Duration
	// 插件提供的受信任账号适配，不来自 RPC。
	LoginOptions   func(context.Context) (AccountOptions, error)
	PrepareAccount func(AccountRequest) AccountRequest
}

func (c *Client) Scope() Scope { return c.scope }

func (c *Client) Capabilities() []Capability {
	return Capabilities(c.platform)
}

// Resolve is deliberately unable to return a URL or headers for audio.
func (c *Client) resolve(ctx context.Context, externalID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return domain.Fail("UNSUPPORTED", "This adapter does not resolve audio resources")
}
