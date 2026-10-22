package pluginproto

import (
	"encoding/json"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// TrackDetails 由来源插件生成，宿主和客户端仅校验及展示纯文本字段。
type TrackDetails struct {
	ExternalID string               `json:"externalId"`
	Title      string               `json:"title"`
	Sections   []TrackDetailSection `json:"sections"`
}

type TrackDetailSection struct {
	ID     string             `json:"id"`
	Title  string             `json:"title"`
	Fields []TrackDetailField `json:"fields"`
}

type TrackDetailField struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Value string `json:"value"`
}

var detailID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{0,63}$`)

// Validate 同时约束对象身份、结构和总大小，拒绝重复字段与无法展示的控制字符。
func (d TrackDetails) Validate(externalID string) error {
	invalid := func() error { return Failure("PROTOCOL_MISMATCH", "Invalid provider track details") }
	if d.ExternalID != externalID || !detailText(d.ExternalID, 128, false) || !detailText(d.Title, 4096, false) || d.Sections == nil || len(d.Sections) > 16 {
		return invalid()
	}
	seen, count := map[string]bool{}, 0
	for _, section := range d.Sections {
		if !detailID.MatchString(section.ID) || seen[section.ID] || !detailText(section.Title, 256, false) || len(section.Fields) < 1 || len(section.Fields) > 64 {
			return invalid()
		}
		seen[section.ID] = true
		fields := map[string]bool{}
		for _, field := range section.Fields {
			count++
			if count > 128 || !detailID.MatchString(field.ID) || fields[field.ID] || !detailText(field.Label, 256, false) || !detailText(field.Value, 16384, true) {
				return invalid()
			}
			fields[field.ID] = true
		}
	}
	encoded, err := json.Marshal(d)
	if err != nil || len(encoded) > 128<<10 {
		return invalid()
	}
	return nil
}

func detailText(value string, max int, multiline bool) bool {
	return strings.TrimSpace(value) != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\r' || r == '\t'))
	})
}
