package online

import (
	"context"
	"crypto/rand"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

var numericID = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var songMID = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

type Client struct {
	platform string
	scope    Scope
	http     *http.Client
	key      [32]byte
	mu       sync.Mutex
	next     time.Time
}

func New(platform string, scope Scope, opts Options) (*Client, error) {
	if platform != Netease && platform != QQMusic && platform != AppleMusic {
		return nil, domain.Fail("INVALID_ARGUMENT", "Unknown online platform")
	}
	for _, s := range []string{scope.UserID, scope.InstanceID} {
		clean, err := textValue(s, 128)
		if err != nil || clean == "" || clean != s {
			return nil, domain.Fail("INVALID_ARGUMENT", "A valid host-owned online scope is required")
		}
	}
	if scope.AccountRef != "" {
		return nil, domain.Fail("UNSUPPORTED", "Account-scoped access is not implemented")
	}
	if opts.Timeout == 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.Timeout < time.Second || opts.Timeout > 30*time.Second {
		return nil, domain.Fail("INVALID_ARGUMENT", "Online request timeout must be between 1 and 30 seconds")
	}
	if opts.Transport == nil {
		opts.Transport = publicTransport()
	}
	c := &Client{platform: platform, scope: scope, http: &http.Client{
		Transport: opts.Transport, Timeout: opts.Timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	if _, err := rand.Read(c.key[:]); err != nil {
		return nil, domain.Fail("TEMPORARY_FAILURE", "Cannot initialize online cursor key")
	}
	return c, nil
}

func (c *Client) Close() { c.http.CloseIdleConnections() }

func (c *Client) admit(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if wait := c.next.Sub(now); wait > 0 {
		seconds := int((wait + time.Second - 1) / time.Second)
		return &domain.Error{Code: "RATE_LIMITED", Message: "Online metadata request rate is limited", RetryAfter: &seconds}
	}
	c.next = now.Add(time.Second)
	return nil
}

func (c *Client) requireNetease(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.platform == AppleMusic {
		return domain.Fail("AUTH_REQUIRED", "Apple Music catalog requires a developer token; this adapter has no credential support")
	}
	if c.platform != Netease {
		return domain.Fail("UNSUPPORTED", "This public metadata capability has not been verified for this platform")
	}
	return nil
}
