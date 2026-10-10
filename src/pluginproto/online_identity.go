package pluginproto

import (
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/language"
)

// PluginIdentity is supplied by the plugin, separately from instance labels and
// signed-in account nicknames. LocalizedNames uses BCP 47 language tags.
type PluginIdentity struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	LocalizedNames map[string]string `json:"localizedNames"`
	Features       []string          `json:"features"`
}

type OnlinePlugin = PluginIdentity

func (p *PluginIdentity) Clone() *PluginIdentity {
	if p == nil {
		return nil
	}
	copy := *p
	copy.LocalizedNames = maps.Clone(p.LocalizedNames)
	copy.Features = slices.Clone(p.Features)
	return &copy
}

var onlineLanguageTag = regexp.MustCompile(`^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8})*$`)
var pluginFeature = regexp.MustCompile(`^[a-z][a-z0-9]*([.-][a-z0-9]+)*$`)

func (p *PluginIdentity) Validate(expectedID string) error {
	if p == nil || expectedID == "" || p.ID != expectedID || !onlineDisplayName(p.Name) || p.LocalizedNames == nil || len(p.LocalizedNames) > 64 || p.Features == nil || len(p.Features) > 32 {
		return Failure("PROTOCOL_MISMATCH", "Invalid plugin display identity")
	}
	seen := map[string]bool{}
	for _, feature := range p.Features {
		if len(feature) > 64 || !pluginFeature.MatchString(feature) || seen[feature] {
			return Failure("PROTOCOL_MISMATCH", "Invalid plugin feature")
		}
		seen[feature] = true
	}
	seen = map[string]bool{}
	for tag, name := range p.LocalizedNames {
		key := strings.ToLower(tag)
		if len(tag) > 63 || !onlineLanguageTag.MatchString(tag) || !onlineDisplayName(name) || seen[key] {
			return Failure("PROTOCOL_MISMATCH", "Invalid plugin localized name")
		}
		if _, err := language.Parse(tag); err != nil {
			return Failure("PROTOCOL_MISMATCH", "Invalid plugin language tag")
		}
		seen[key] = true
	}
	return nil
}

func onlineDisplayName(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

// Clone returns a detached copy of a validated, JSON-compatible descriptor.
func (d OnlineDescriptor) Clone() OnlineDescriptor {
	encoded, _ := json.Marshal(d)
	var copy OnlineDescriptor
	_ = json.Unmarshal(encoded, &copy)
	return copy
}
