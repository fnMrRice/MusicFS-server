package pluginproto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/instanceid"
	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

const OnlineRevision = "online-declared-v4"

type OnlineHello struct {
	Hello
	UserID    string `json:"userId"`
	TimeoutMs int64  `json:"timeoutMs"`
}

type OnlineScope struct {
	Context Context `json:"context"`
}
type OnlineSearch struct {
	Kind    string  `json:"kind"`
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
	DownloadQualities *QualityOptions         `json:"downloadQualities,omitempty"`
	Plugin            *OnlinePlugin           `json:"plugin,omitempty"`
	Platform          string                  `json:"platform"`
	SourceType        string                  `json:"sourceType"`
	Authentication    string                  `json:"authentication"`
	Capabilities      []online.Capability     `json:"capabilities"`
	Resources         online.ResourcePolicy   `json:"resources"`
	Login             *online.LoginDescriptor `json:"login,omitempty"`
	SchemaRevision    string                  `json:"schemaRevision"`
	ConfigSchema      map[string]any          `json:"configSchema"`
	ConfigDefaults    map[string]any          `json:"configDefaults"`
}

// OnlinePluginID 只校验平台标识格式；插件准入由宿主受信任注册表管理。
func OnlinePluginID(platform string) string {
	if !regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`).MatchString(platform) {
		return ""
	}
	return "musicfs.plugin.source." + platform
}

// OnlinePolicy 提供平台无关的空描述符；能力和资料策略必须由插件补充。
func OnlinePolicy(platform string) OnlineDescriptor {
	return OnlineDescriptor{
		Platform: platform, SourceType: SourceNetwork, Authentication: "platformAccountRequired", Capabilities: []online.Capability{}, SchemaRevision: OnlineRevision,
		ConfigSchema: map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{}}, ConfigDefaults: map[string]any{},
	}
}

func (d OnlineDescriptor) Validate(platform string) error {
	if OnlinePluginID(platform) == "" {
		return Failure("PROTOCOL_MISMATCH", "Unknown online platform")
	}
	if err := d.Plugin.Validate(OnlinePluginID(platform)); err != nil {
		return err
	}
	if !slices.Contains(d.Plugin.Features, "music-source") {
		return Failure("PROTOCOL_MISMATCH", "Online plugin must declare the music-source feature")
	}
	if err := validateOnlineCapabilities(d.Capabilities); err != nil {
		return err
	}
	if err := d.DownloadQualities.validate(); err != nil {
		return err
	}
	if d.Platform != platform || d.SourceType != SourceNetwork || d.SchemaRevision != OnlineRevision || (d.Authentication != "anonymous" && d.Authentication != "platformAccountRequired") || d.Resources.Validate() != nil || d.Login.Validate() != nil {
		return Failure("PROTOCOL_MISMATCH", "Invalid online descriptor")
	}
	if d.ConfigSchema == nil || d.ConfigDefaults == nil {
		return Failure("PROTOCOL_MISMATCH", "Online configuration declaration is required")
	}
	return validateOnlineConfiguration(d)
}

// OnlineCapabilities 从插件声明导出实际启用能力，不维护任何平台能力上限。
func OnlineCapabilities(descriptor OnlineDescriptor) []string {
	result := []string{}
	for _, c := range descriptor.Capabilities {
		if c.Supported {
			result = append(result, c.Name)
		}
	}
	return result
}

func ValidateOnlineManifest(m Manifest, platform string) error {
	if m.Validate("provider") != nil || m.SourceType != SourceNetwork || OnlinePluginID(platform) == "" || !m.SupportsMultipleInstances || m.PluginID != OnlinePluginID(platform) || !ValidPluginVersion(m.PluginVersion) || !reflect.DeepEqual(m.Roles, []string{"provider"}) || !onlineCapabilitySubset(m.Capabilities, online.CapabilityNames()) || m.Configuration != nil || m.MaxPageItems != online.MaxPageItems || m.MaxMessageBytes != MaxMessageBytes {
		return Failure("PROTOCOL_MISMATCH", "Online manifest does not match the expected platform")
	}
	return nil
}

var onlineNonce = regexp.MustCompile(`^[0-9a-f]{48}$`)

func (h OnlineHello) Validate() error {
	if h.ProtocolVersion != ProtocolVersion || h.ConfigRevision != OnlineRevision || h.TaskID != "" || !onlineNonce.MatchString(h.Nonce) || !instanceid.Valid(h.InstanceID) || !OnlineIdentity(h.UserID) || h.TimeoutMs < 1000 || h.TimeoutMs > 30000 {
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
