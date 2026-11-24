package online

import (
	"encoding/json"
	"regexp"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
)

// AccountView is the only account data exposed to HTTP consumers.
type AccountView struct {
	Revision         string     `json:"revision,omitempty"`
	State            string     `json:"state"`
	AccountID        string     `json:"accountId,omitempty"`
	DisplayName      string     `json:"displayName,omitempty"`
	AvatarURL        string     `json:"avatarUrl,omitempty"`
	MembershipTier   string     `json:"membershipTier,omitempty"`
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
	View                  AccountView          `json:"view"`
	Secret                json.RawMessage      `json:"secret,omitempty"`
	DisplayCount          *plugindisplay.Count `json:"displayCount,omitempty"`
	RecommendationBinding string               `json:"recommendationBinding,omitempty"`
}

// VerifiedAt 判断该时间点是否存在已校验且未过期的账号。
func (v AccountView) VerifiedAt(now time.Time) bool {
	return v.State == "signedIn" && v.AccountID != "" && v.Revision != "" && (v.ExpiresAt == nil || now.Before(*v.ExpiresAt))
}

// VerifiedMembershipTier 只公开当前已验证账号的已知会员档位。
func (v AccountView) VerifiedMembershipTier(now time.Time) string {
	if v.VerifiedAt(now) {
		switch v.MembershipTier {
		case "free", "vip", "svip":
			return v.MembershipTier
		}
	}
	return ""
}

var recommendationBindingPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// RecommendationScope 只从当前账号状态获取推荐范围，缺少持久绑定时沿用登录代次。
func (r AccountResult) RecommendationScope() (account, revision, binding string, err error) {
	if !r.View.VerifiedAt(time.Now()) {
		return "", "", "", fault.Fail("PLATFORM_AUTH_REQUIRED", "Sign in to the music platform")
	}
	binding = r.RecommendationBinding
	if binding == "" {
		binding = r.View.Revision
	} else if !recommendationBindingPattern.MatchString(binding) {
		return "", "", "", fault.Fail("PROTOCOL_MISMATCH", "Invalid recommendation account binding")
	}
	return r.View.AccountID, r.View.Revision, binding, nil
}
