package contenttype

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
)

const (
	maxIdentifierLength  = 64
	maxPluginIDLength    = 128
	maxNameLength        = 128
	maxDescriptionLength = 2048
	maxNamedValues       = 64
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
	pluginIDPattern   = regexp.MustCompile(`^[a-z][a-z0-9_-]*(\.[a-z][a-z0-9_-]*)+$`)
)

// Validate 校验声明，不隐式修剪、重命名或补齐字段。
// 类型、角色和命名空间标识符必须以小写字母开头，仅含小写字母、数字、下划线和连字符。
// 名称按 Unicode 字符计数；集合保持声明顺序，角色和命名空间分别要求标识符唯一。
func (d Descriptor) Validate() error {
	if !ValidID(d.ID) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid content type id")
	}
	if len(d.PluginID) > maxPluginIDLength || !pluginIDPattern.MatchString(d.PluginID) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid content type plugin id")
	}
	if !pluginproto.ValidPluginVersion(d.PluginVersion) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid content type plugin version")
	}
	if !validText(d.Name, maxNameLength, false, false) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid content type name")
	}
	if !validText(d.Description, maxDescriptionLength, true, true) {
		return fmt.Errorf("INVALID_ARGUMENT: invalid content type description")
	}
	if err := validateNamedValues("credit role", d.CreditRoles); err != nil {
		return err
	}
	if err := validateNamedValues("external id namespace", d.ExternalIDNamespaces); err != nil {
		return err
	}
	if len(d.Views) == 0 || len(d.Views) > 2 {
		return fmt.Errorf("INVALID_ARGUMENT: invalid content type view count")
	}
	seen := make(map[string]bool, len(d.Views))
	for _, view := range d.Views {
		if view != ViewWorkList && view != ViewWorkDetail {
			return fmt.Errorf("INVALID_ARGUMENT: unsupported content type view")
		}
		if seen[view] {
			return fmt.Errorf("INVALID_ARGUMENT: duplicate content type view")
		}
		seen[view] = true
	}
	return nil
}

func validateNamedValues(kind string, values []NamedValue) error {
	if len(values) > maxNamedValues {
		return fmt.Errorf("INVALID_ARGUMENT: too many content type %s entries", kind)
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !ValidID(value.ID) || !validText(value.Name, maxNameLength, false, false) {
			return fmt.Errorf("INVALID_ARGUMENT: invalid content type %s", kind)
		}
		if seen[value.ID] {
			return fmt.Errorf("INVALID_ARGUMENT: duplicate content type %s", kind)
		}
		seen[value.ID] = true
	}
	return nil
}

// ValidID 校验类型、角色与命名空间使用的稳定标识符，最多 64 个 ASCII 字符。
// 标识符以小写字母开头，后续只允许小写字母、数字、下划线和连字符，不允许点号。
func ValidID(value string) bool {
	return len(value) <= maxIdentifierLength && identifierPattern.MatchString(value)
}

func validText(value string, limit int, optional, multiline bool) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || strings.TrimSpace(value) != value {
		return false
	}
	if value == "" {
		return optional
	}
	for _, char := range value {
		if unicode.IsControl(char) && !(multiline && (char == '\n' || char == '\r' || char == '\t')) {
			return false
		}
	}
	return true
}
