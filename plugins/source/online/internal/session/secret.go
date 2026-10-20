// Package session contains plugin-private persisted account state.
package session

// Secret is never returned through a public HTTP response. The host persists its
// opaque serialized form under the account owner and instance scope.
type Secret struct {
	Platform       string            `json:"platform"`
	UserID         string            `json:"userId"`
	InstanceID     string            `json:"instanceId"`
	DeviceID       string            `json:"deviceId"`
	AccountID      string            `json:"accountId,omitempty"`
	Name           string            `json:"name,omitempty"`
	AvatarURL      string            `json:"avatarUrl,omitempty"`
	Cookies        map[string]string `json:"cookies,omitempty"`
	MusicKey       string            `json:"musicKey,omitempty"`
	UserToken      string            `json:"userToken,omitempty"`
	DeveloperToken string            `json:"developerToken,omitempty"`
}

func ValidToken(value string) bool {
	if len(value) == 0 || len(value) > 16384 {
		return false
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return false
		}
	}
	return true
}
