package pluginproto

import (
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/online"
)

// ValidateOnlineDescriptor verifies the independently received descriptor and
// manifest together. Hosts must call this before publishing worker capabilities.
func ValidateOnlineDescriptor(m Manifest, d OnlineDescriptor, platform string) error {
	if err := ValidateOnlineManifest(m, platform); err != nil {
		return err
	}
	if err := d.Validate(platform); err != nil {
		return err
	}
	enabled := []string{}
	for _, capability := range d.Capabilities {
		if capability.Supported {
			enabled = append(enabled, capability.Name)
		}
	}
	if !reflect.DeepEqual(m.Plugin, d.Plugin) || len(enabled) != len(m.Capabilities) || !onlineCapabilitySubset(enabled, m.Capabilities) {
		return Failure("PROTOCOL_MISMATCH", "Online descriptor and manifest disagree")
	}
	return nil
}

func validateOnlineCapabilities(platform string, capabilities []online.Capability) error {
	policy := map[string]bool{}
	for _, capability := range online.Capabilities(platform) {
		policy[capability.Name] = capability.Supported
	}
	seen := map[string]bool{}
	if capabilities == nil || len(capabilities) > len(policy) {
		return Failure("PROTOCOL_MISMATCH", "Invalid online capability declaration")
	}
	for _, capability := range capabilities {
		allowed, known := policy[capability.Name]
		if !known || seen[capability.Name] || capability.Supported && !allowed || !utf8.ValidString(capability.Reason) || utf8.RuneCountInString(capability.Reason) < 1 || utf8.RuneCountInString(capability.Reason) > 2048 || strings.ContainsFunc(capability.Reason, unicode.IsControl) {
			return Failure("PROTOCOL_MISMATCH", "Online capability exceeds verified host policy")
		}
		seen[capability.Name] = true
	}
	return nil
}

func onlineCapabilitySubset(capabilities, allowed []string) bool {
	if capabilities == nil {
		return false
	}
	known := map[string]bool{}
	for _, name := range allowed {
		known[name] = true
	}
	seen := map[string]bool{}
	for _, name := range capabilities {
		if !known[name] || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}
