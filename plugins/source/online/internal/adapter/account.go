package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/internal/session"
	domain "github.com/fnMrRice/MusicFS-server/sdk/fault"
)

type accountState struct {
	revoked  atomic.Bool
	active   atomic.Pointer[context.CancelFunc]
	canceled atomic.Pointer[string]
	mu       sync.Mutex
	view     AccountView
	secret   accountSecret
	pending  accountSecret
	key      string
	next     time.Time
}
type accountSecret = session.Secret

func (c *Client) Account(ctx context.Context, r AccountRequest) (result AccountResult, err error) {
	if c.options.PrepareAccount != nil {
		r = c.options.PrepareAccount(r)
	}
	defer func() {
		if err == nil {
			result.DisplayCount = accountDisplayCount(result.View)
		}
	}()
	a := &c.account
	if r.Action == "logout" || (r.Action == "signalCancel" && r.LoginID == "*") {
		a.revoked.Store(true)
		if cancel := a.active.Load(); cancel != nil {
			(*cancel)()
		}
	}
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
		secret.AvatarURL = c.driver.AvatarURL(secret.AvatarURL)
		a.secret = secret
		a.revoked.Store(false)
		a.view = c.savedAccountView()
	case "state":
		if a.revoked.Load() {
			return AccountResult{View: AccountView{State: "signedOut"}}, nil
		}
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
		err = c.driver.Begin(ctx, r)
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
		err = c.driver.Poll(ctx, r)
		if err != nil {
			return AccountResult{}, err
		}
		if a.view.LoginID != "" && a.view.ExpiresAt != nil && !time.Now().Before(*a.view.ExpiresAt) {
			a.pending = accountSecret{}
			a.key = ""
			a.view = AccountView{State: "expired"}
		}
	case "check":
		revision := a.view.Revision
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
		err = c.driver.Check(ctx, &a.secret)
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
			a.view.Revision = revision
			if a.view.Revision == "" {
				a.view.Revision = domain.NewID()
			}
		}
	default:
		return AccountResult{}, domain.Fail("INVALID_ARGUMENT", "Unknown platform account operation")
	}
	result = AccountResult{View: a.view}
	if r.Action == "state" && a.view.VerifiedAt(time.Now()) && a.secret.DeviceID != "" {
		binding := sha256.Sum256([]byte(a.secret.DeviceID))
		result.RecommendationBinding = hex.EncodeToString(binding[:])
	}
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
	return AccountView{State: "unverified", AccountID: s.AccountID, DisplayName: s.Name, AvatarURL: c.driver.AvatarURL(s.AvatarURL)}
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
	a.view.Revision = domain.NewID()
	a.revoked.Store(false)
}
func accountRateLimit() error {
	n := 3
	return &domain.Error{Code: "RATE_LIMITED", Message: "Wait before the next platform login request", RetryAfter: &n}
}
func addAccountCookies(req *http.Request, cookies map[string]string) {
	for name, value := range cookies {
		if value != "" {
			req.AddCookie(&http.Cookie{Name: name, Value: value})
		}
	}
}
