package pluginproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/online"
)

const OnlineRevision = "online-anonymous-v1"

type OnlineHello struct {
	Hello
	UserID    string `json:"userId"`
	TimeoutMs int64  `json:"timeoutMs"`
}

type OnlineScope struct {
	Context Context `json:"context"`
}
type OnlineSearch struct {
	Context Context `json:"context"`
	Query   string  `json:"query"`
	Limit   int     `json:"limit"`
	Cursor  string  `json:"cursor"`
}
type OnlineReference struct {
	Context    Context `json:"context"`
	ExternalID string  `json:"externalId"`
}
type OnlineRecommendations struct {
	Context Context `json:"context"`
	Limit   int     `json:"limit"`
}
type OnlineResult[T any] struct {
	Context Context `json:"context"`
	Value   T       `json:"value"`
}

type OnlineDescriptor struct {
	Platform       string              `json:"platform"`
	Authentication string              `json:"authentication"`
	Capabilities   []online.Capability `json:"capabilities"`
	SchemaRevision string              `json:"schemaRevision"`
	ConfigSchema   map[string]any      `json:"configSchema"`
	ConfigDefaults map[string]any      `json:"configDefaults"`
}

func OnlinePluginID(platform string) string {
	switch platform {
	case online.Netease, online.QQMusic, online.AppleMusic:
		return "musicfs." + platform
	default:
		return ""
	}
}

func DescribeOnline(platform string) OnlineDescriptor {
	auth := "anonymous"
	if platform == online.AppleMusic {
		auth = "developerTokenRequired"
	}
	return OnlineDescriptor{
		Platform: platform, Authentication: auth, Capabilities: online.Capabilities(platform), SchemaRevision: OnlineRevision,
		ConfigSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}, ConfigDefaults: map[string]any{},
	}
}

func (d OnlineDescriptor) Validate(platform string) error {
	if OnlinePluginID(platform) == "" {
		return Failure("PROTOCOL_MISMATCH", "Unknown online platform")
	}
	// JSON round trips normalize map numeric types and reject hidden extensions.
	want, _ := json.Marshal(DescribeOnline(platform))
	actual, err := json.Marshal(d)
	if err != nil || !bytes.Equal(want, actual) {
		return Failure("PROTOCOL_MISMATCH", "Online descriptor does not match the verified adapter revision")
	}
	return nil
}

func OnlineCapabilities(platform string) []string {
	result := []string{}
	for _, c := range online.Capabilities(platform) {
		if c.Supported {
			result = append(result, c.Name)
		}
	}
	return result
}

func ValidateOnlineManifest(m Manifest, platform string) error {
	if m.Validate("provider") != nil || m.PluginID != OnlinePluginID(platform) || m.PluginVersion != "0.1.0" || !reflect.DeepEqual(m.Roles, []string{"provider"}) || !reflect.DeepEqual(m.Capabilities, OnlineCapabilities(platform)) || m.Configuration != nil || m.MaxPageItems != online.MaxPageItems || m.MaxMessageBytes != MaxMessageBytes {
		return Failure("PROTOCOL_MISMATCH", "Online manifest does not match the expected platform")
	}
	return nil
}

var onlineNonce = regexp.MustCompile(`^[0-9a-f]{48}$`)

func (h OnlineHello) Validate() error {
	if h.ProtocolVersion != ProtocolVersion || h.ConfigRevision != OnlineRevision || !onlineNonce.MatchString(h.Nonce) || !OnlineIdentity(h.InstanceID) || !OnlineIdentity(h.UserID) || h.TimeoutMs < 1000 || h.TimeoutMs > 30000 {
		return Failure("PROTOCOL_MISMATCH", "Invalid online handshake")
	}
	return nil
}

func (h OnlineHello) ValidateScope(c Context) error {
	if c.InstanceID != h.InstanceID || c.UserID != h.UserID || c.AccountRef != "" || !onlineNonce.MatchString(c.TaskID) || c.TimeoutMs < 1 || c.TimeoutMs > h.TimeoutMs {
		return Failure("ACCESS_DENIED", "Online request scope mismatch")
	}
	return nil
}

func OnlineIdentity(s string) bool {
	return len(s) > 0 && len(s) <= 128 && utf8.ValidString(s) && strings.TrimSpace(s) == s && !strings.ContainsFunc(s, unicode.IsControl)
}

func DecodeOnline(raw []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return Failure("PROTOCOL_MISMATCH", "Invalid online message")
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("PROTOCOL_MISMATCH: trailing online message data")
	}
	return nil
}
