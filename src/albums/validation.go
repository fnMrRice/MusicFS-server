package albums

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/src/domain"
)

func ValidID(id string) bool {
	return id != "" && len(id) <= 128 && utf8.ValidString(id) && strings.IndexFunc(id, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}
func (t Target) Validate() error {
	if (t.Kind != "sourceAlbum" && t.Kind != "release") || !ValidID(t.ID) {
		return domain.Fail("INVALID_ARGUMENT", "Invalid album cover target")
	}
	return nil
}
func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 256 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", domain.Fail("INVALID_ARGUMENT", "Invalid album or release name")
	}
	return name, nil
}
func NormalizeMembers(ids []string, limit int) ([]string, error) {
	if len(ids) > limit {
		return nil, domain.Fail("INVALID_ARGUMENT", "Album membership limit exceeded")
	}
	result := append([]string{}, ids...)
	seen := make(map[string]bool, len(result))
	for _, id := range result {
		if !ValidID(id) || seen[id] {
			return nil, domain.Fail("INVALID_ARGUMENT", "Invalid or duplicate album member")
		}
		seen[id] = true
	}
	return result, nil
}
