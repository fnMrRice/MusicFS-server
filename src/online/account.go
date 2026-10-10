package online

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

// AccountView is the only account data exposed to HTTP consumers.
type AccountView struct {
	State            string     `json:"state"`
	AccountID        string     `json:"accountId,omitempty"`
	DisplayName      string     `json:"displayName,omitempty"`
	LoginID          string     `json:"loginId,omitempty"`
	Method           string     `json:"method,omitempty"`
	QRURL            string     `json:"qrUrl,omitempty"`
	QRImage          string     `json:"qrImage,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	PollAfterSeconds int        `json:"pollAfterSeconds,omitempty"`
}

// AccountRequest crosses the private Host/Worker channel. Secret is never
// returned by an HTTP route. Only Apple completion accepts a user token.
type AccountRequest struct {
	Action         string          `json:"action"`
	LoginID        string          `json:"loginId,omitempty"`
	UserToken      string          `json:"userToken,omitempty"`
	DeveloperToken string          `json:"developerToken,omitempty"`
	Secret         json.RawMessage `json:"secret,omitempty"`
}
type AccountResult struct {
	View   AccountView     `json:"view"`
	Secret json.RawMessage `json:"secret,omitempty"`
}
type accountState struct {
	canceled atomic.Pointer[string]
	mu       sync.Mutex
	view     AccountView
	secret   accountSecret
	pending  accountSecret
	key      string
	next     time.Time
}
type accountSecret struct {
	Platform       string            `json:"platform"`
	UserID         string            `json:"userId"`
	InstanceID     string            `json:"instanceId"`
	DeviceID       string            `json:"deviceId"`
	AccountID      string            `json:"accountId,omitempty"`
	Name           string            `json:"name,omitempty"`
	Cookies        map[string]string `json:"cookies,omitempty"`
	MusicKey       string            `json:"musicKey,omitempty"`
	UserToken      string            `json:"userToken,omitempty"`
	DeveloperToken string            `json:"developerToken,omitempty"`
}

func (c *Client) Account(ctx context.Context, r AccountRequest) (AccountResult, error) {
	a := &c.account
	if (r.Action == "cancel" || r.Action == "signalCancel") && r.LoginID != "" {
		id := r.LoginID
		a.canceled.Store(&id)
	}
	if r.Action == "signalCancel" {
		return AccountResult{View: AccountView{State: "canceled"}}, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AccountResult{}, err
	}
	if a.view.State == "" {
		a.view.State = "signedOut"
	}
	if a.view.ExpiresAt != nil && !time.Now().Before(*a.view.ExpiresAt) && a.view.LoginID != "" {
		a.view = AccountView{State: "expired"}
		a.pending = accountSecret{}
		a.key = ""
	}
	switch r.Action {
	case "restore":
		var secret accountSecret
		if len(r.Secret) > 64<<10 || json.Unmarshal(r.Secret, &secret) != nil || secret.Platform != c.platform || secret.UserID != c.scope.UserID || secret.InstanceID != c.scope.InstanceID || secret.AccountID == "" {
			return AccountResult{}, domain.Fail("ACCESS_DENIED", "Stored platform account scope is invalid")
		}
		a.secret = secret
		a.view = AccountView{State: "unverified", AccountID: secret.AccountID, DisplayName: secret.Name}
	case "state":
	case "cancel":
		if a.view.State == "canceled" {
			return AccountResult{View: a.view}, nil
		}
		if r.LoginID == "" || a.view.LoginID != r.LoginID {
			return AccountResult{}, domain.Fail("NOT_FOUND", "Login challenge was not found")
		}
		a.pending = accountSecret{}
		a.key = ""
		a.view = c.savedAccountView()
	case "logout":
		// Local revocation is unconditional, including when the platform is offline.
		a.secret = accountSecret{}
		a.pending = accountSecret{}
		a.key = ""
		a.view = AccountView{State: "signedOut"}
	case "begin":
		if a.view.LoginID != "" {
			return AccountResult{View: a.view}, nil
		}
		if a.secret.AccountID != "" {
			return AccountResult{}, domain.Fail("INVALID_ARGUMENT", "Sign out the current platform account before starting another login")
		}
		if time.Now().Before(a.next) {
			return AccountResult{}, accountRateLimit()
		}
		a.next = time.Now().Add(3 * time.Second)
		if err := c.admit(ctx); err != nil {
			return AccountResult{}, err
		}
		a.canceled.Store(nil)
		a.pending = accountSecret{Platform: c.platform, UserID: c.scope.UserID, InstanceID: c.scope.InstanceID, DeviceID: domain.NewID(), Cookies: map[string]string{}}
		until := time.Now().UTC().Add(3 * time.Minute)
		a.view = AccountView{State: "waiting", LoginID: domain.NewID(), ExpiresAt: &until, PollAfterSeconds: 3}
		var err error
		switch c.platform {
		case Netease:
			err = c.beginNetease(ctx)
		case QQMusic:
			err = c.beginQQ(ctx)
		case AppleMusic:
			if !validAccountToken(r.DeveloperToken) {
				err = domain.Fail("AUTH_REQUIRED", "Apple Music developer configuration is required")
			} else {
				a.pending.DeveloperToken = r.DeveloperToken
				a.view.Method = "musicKit"
			}
		}
		if err != nil {
			a.pending = accountSecret{}
			a.view = AccountView{State: "signedOut"}
			return AccountResult{}, err
		}
	case "poll", "complete":
		if r.LoginID == "" || r.LoginID != a.view.LoginID || (a.view.State != "waiting" && a.view.State != "scanned") {
			return AccountResult{}, domain.Fail("NOT_FOUND", "Login challenge is no longer active")
		}
		if time.Now().Before(a.next) {
			return AccountResult{}, accountRateLimit()
		}
		a.next = time.Now().Add(3 * time.Second)
		var err error
		if err = c.admit(ctx); err != nil {
			return AccountResult{}, err
		}
		switch c.platform {
		case Netease:
			err = c.pollNetease(ctx)
		case QQMusic:
			err = c.pollQQ(ctx)
		case AppleMusic:
			if r.Action != "complete" || !validAccountToken(r.UserToken) {
				return AccountResult{}, domain.Fail("INVALID_ARGUMENT", "MusicKit user authorization is required")
			}
			a.pending.UserToken = r.UserToken
			err = c.checkApple(ctx, &a.pending)
			if err == nil {
				c.completeAccount()
			}
		}
		if err != nil {
			return AccountResult{}, err
		}
		if a.view.LoginID != "" && a.view.ExpiresAt != nil && !time.Now().Before(*a.view.ExpiresAt) {
			a.pending = accountSecret{}
			a.key = ""
			a.view = AccountView{State: "expired"}
		}
	case "check":
		if a.secret.AccountID == "" {
			return AccountResult{View: a.view}, nil
		}
		if time.Now().Before(a.next) {
			return AccountResult{}, accountRateLimit()
		}
		a.next = time.Now().Add(3 * time.Second)
		var err error
		if err = c.admit(ctx); err != nil {
			return AccountResult{}, err
		}
		switch c.platform {
		case Netease:
			err = c.checkNetease(ctx, &a.secret)
		case QQMusic:
			err = c.checkQQ(ctx, &a.secret)
		case AppleMusic:
			err = c.checkApple(ctx, &a.secret)
		}
		if err != nil {
			var failure *domain.Error
			if errors.As(err, &failure) && (failure.Code == "AUTH_REQUIRED" || failure.Code == "ACCESS_DENIED") {
				a.secret = accountSecret{}
				a.view = AccountView{State: "expired"}
			} else {
				return AccountResult{}, err
			}
		} else {
			a.view = c.savedAccountView()
			a.view.State = "signedIn"
		}
	default:
		return AccountResult{}, domain.Fail("INVALID_ARGUMENT", "Unknown platform account operation")
	}
	result := AccountResult{View: a.view}
	if a.secret.AccountID != "" {
		result.Secret, _ = json.Marshal(a.secret)
	}
	return result, nil
}

func (c *Client) savedAccountView() AccountView {
	s := c.account.secret
	if s.AccountID == "" {
		return AccountView{State: "signedOut"}
	}
	return AccountView{State: "unverified", AccountID: s.AccountID, DisplayName: s.Name}
}
func (c *Client) completeAccount() {
	a := &c.account
	if id := a.canceled.Load(); id != nil && (*id == a.view.LoginID || *id == "*") {
		a.pending = accountSecret{}
		a.key = ""
		a.view = AccountView{State: "canceled"}
		return
	}
	if a.view.ExpiresAt == nil || !time.Now().Before(*a.view.ExpiresAt) {
		a.pending = accountSecret{}
		a.key = ""
		a.view = AccountView{State: "expired"}
		return
	}
	a.secret = a.pending
	a.pending = accountSecret{}
	a.key = ""
	a.view = c.savedAccountView()
	a.view.State = "signedIn"
}
func accountRateLimit() error {
	n := 3
	return &domain.Error{Code: "RATE_LIMITED", Message: "Wait before the next platform login request", RetryAfter: &n}
}
func validAccountToken(v string) bool {
	return len(v) > 0 && len(v) <= 16384 && !containsUnsafeToken(v)
}
func containsUnsafeToken(v string) bool {
	for _, r := range v {
		if r < 33 || r > 126 {
			return true
		}
	}
	return false
}

func addAccountCookies(req *http.Request, cookies map[string]string) {
	for name, value := range cookies {
		if value != "" {
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
	}
}
