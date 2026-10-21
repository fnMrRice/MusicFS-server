package online

import (
	"encoding/json"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/plugindisplay"
)

// AccountView is the only account data exposed to HTTP consumers.
type AccountView struct {
	Revision         string     `json:"revision,omitempty"`
	State            string     `json:"state"`
	AccountID        string     `json:"accountId,omitempty"`
	DisplayName      string     `json:"displayName,omitempty"`
	AvatarURL        string     `json:"avatarUrl,omitempty"`
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
	View         AccountView          `json:"view"`
	Secret       json.RawMessage      `json:"secret,omitempty"`
	DisplayCount *plugindisplay.Count `json:"displayCount,omitempty"`
}
