package plugindisplay

import (
	"encoding/json"
	"testing"
)

func TestCountValidationAndDetachedClone(t *testing.T) {
	valid := &Count{Value: MaxValue, Unit: "users", LocalizedUnits: map[string]string{"zh-Hans": "个用户", "ja": "ユーザー"}}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	copy := valid.Clone()
	copy.LocalizedUnits["zh-Hans"] = "changed"
	if valid.LocalizedUnits["zh-Hans"] != "个用户" {
		t.Fatal("clone retained a mutable translation map")
	}
	for name, mutate := range map[string]func(*Count){
		"negative":             func(c *Count) { c.Value = -1 },
		"unsafe integer":       func(c *Count) { c.Value = MaxValue + 1 },
		"empty unit":           func(c *Count) { c.Unit = "" },
		"unit whitespace":      func(c *Count) { c.Unit = " users" },
		"unit control":         func(c *Count) { c.Unit = "users\n" },
		"missing translations": func(c *Count) { c.LocalizedUnits = nil },
		"invalid language":     func(c *Count) { c.LocalizedUnits["zh_Hans"] = "个用户" },
		"unknown language":     func(c *Count) { c.LocalizedUnits["invalidlanguage"] = "users" },
		"duplicate language":   func(c *Count) { c.LocalizedUnits["ZH-hans"] = "个用户" },
		"invalid translation":  func(c *Count) { c.LocalizedUnits["en"] = "\x00" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid.Clone()
			mutate(candidate)
			if candidate.Validate() == nil {
				t.Fatal("invalid plugin count was accepted")
			}
		})
	}
	var absent *Count
	if absent.Clone() != nil || absent.Validate() == nil {
		t.Fatal("absent statistic must stay absent and cannot be validated")
	}
	if (&Count{Unit: "sources", LocalizedUnits: map[string]string{}}).Validate() != nil {
		t.Fatal("zero count and no translations must be valid")
	}
}

func TestDecodeRequiresCompleteValidStatistic(t *testing.T) {
	for _, value := range []string{
		`null`, `"count"`, `{"unit":"users","localizedUnits":{}}`,
		`{"value":0,"unit":"users","localizedUnits":null}`,
		`{"value":1.5,"unit":"users","localizedUnits":{}}`,
		`{"value":0,"unit":"users","localizedUnits":{},"extra":true}`,
		`{"value":0,"unit":"users","localizedUnits":{}} {}`,
	} {
		if count, err := Decode(json.RawMessage(value)); err == nil || count != nil {
			t.Fatal("malformed statistic was accepted", value)
		}
	}
	if count, err := Decode(json.RawMessage(`{"value":0,"unit":"users","localizedUnits":{}}`)); err != nil || count.Value != 0 {
		t.Fatal("plugin zero was not preserved", count, err)
	}
}
