// Package plugindisplay defines optional, plugin-owned display statistics.
package plugindisplay

import (
	"bytes"
	"encoding/json"
	"maps"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
	"golang.org/x/text/language"
)

const MaxValue int64 = 9007199254740991

type Count struct {
	Value          int64             `json:"value"`
	Unit           string            `json:"unit"`
	LocalizedUnits map[string]string `json:"localizedUnits"`
}

// Decode validates an optional statistic separately from its enclosing result.
func Decode(raw json.RawMessage) (*Count, error) {
	if !json.Valid(raw) {
		return nil, invalid()
	}
	var wire struct {
		Value          *int64            `json:"value"`
		Unit           *string           `json:"unit"`
		LocalizedUnits map[string]string `json:"localizedUnits"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&wire) != nil || wire.Value == nil || wire.Unit == nil {
		return nil, invalid()
	}
	count := &Count{Value: *wire.Value, Unit: *wire.Unit, LocalizedUnits: wire.LocalizedUnits}
	if err := count.Validate(); err != nil {
		return nil, err
	}
	return count, nil
}

func (c *Count) Clone() *Count {
	if c == nil {
		return nil
	}
	copy := *c
	copy.LocalizedUnits = maps.Clone(c.LocalizedUnits)
	return &copy
}

var languageTag = regexp.MustCompile(`^[A-Za-z]{1,8}(-[A-Za-z0-9]{1,8})*$`)

func (c *Count) Validate() error {
	if c == nil || c.Value < 0 || c.Value > MaxValue || !validText(c.Unit) || c.LocalizedUnits == nil || len(c.LocalizedUnits) > 64 {
		return invalid()
	}
	seen := map[string]bool{}
	for tag, unit := range c.LocalizedUnits {
		key := strings.ToLower(tag)
		if len(tag) > 63 || !languageTag.MatchString(tag) || seen[key] || !validText(unit) {
			return invalid()
		}
		if _, err := language.Parse(tag); err != nil {
			return invalid()
		}
		seen[key] = true
	}
	return nil
}

func validText(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) > 0 && utf8.RuneCountInString(value) <= 128 && strings.TrimSpace(value) == value && !strings.ContainsFunc(value, unicode.IsControl)
}

func invalid() error {
	return fault.Fail("PROTOCOL_MISMATCH", "Invalid plugin display count")
}
