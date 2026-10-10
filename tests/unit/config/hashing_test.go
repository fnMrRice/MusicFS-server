package config_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fnMrRice/MusicFS-server/tests/testkit"
	"go.yaml.in/yaml/v3"
)

func TestHashingDefaultsAndPartialOverrides(t *testing.T) {
	for _, suffix := range []string{"", "hashing: {}\n", "hashing: {auto: false}\n"} {
		doc, err := parse(t, minimalYAML+suffix)
		if err != nil {
			t.Fatal(err)
		}
		if c := doc.Config.Hashing; c.Auto || c.MaxConcurrent != 1 || c.BytesPerSecond != 4194304 {
			t.Fatalf("unexpected hashing defaults: %+v", c)
		}
	}
	doc, err := parse(t, minimalYAML+"hashing: {auto: true, maxConcurrent: 4}\n")
	if err != nil || !doc.Config.Hashing.Auto || doc.Config.Hashing.MaxConcurrent != 4 || doc.Config.Hashing.BytesPerSecond != 4194304 {
		t.Fatalf("partial hashing override failed: %v", err)
	}
}

func TestHashingConfigurationBoundsAndSchema(t *testing.T) {
	schema := configurationSchema(t)
	for _, concurrency := range []int{0, 1, 4, 5} {
		for _, speed := range []int64{65535, 65536, 4194304, 1073741824, 1073741825} {
			t.Run(fmt.Sprintf("%d-%d", concurrency, speed), func(t *testing.T) {
				raw := minimalYAML + fmt.Sprintf("hashing: {auto: false, maxConcurrent: %d, bytesPerSecond: %d}\n", concurrency, speed)
				valid := concurrency >= 1 && concurrency <= 4 && speed >= 65536 && speed <= 1073741824
				_, err := parse(t, raw)
				if (err == nil) != valid {
					t.Fatalf("unexpected parser validity: %v", err)
				}
				var value any
				if err := yaml.Unmarshal([]byte(raw), &value); err != nil {
					t.Fatal(err)
				}
				body, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err := schema.Validate(testkit.DecodeJSON(t, body)); (err == nil) != valid {
					t.Fatalf("unexpected schema validity: %v", err)
				}
			})
		}
	}
}

func TestHashingStrictYAML(t *testing.T) {
	for name, field := range map[string]string{
		"unknown": "{PRIVATE-SENTINEL: true}", "null": "null", "sequence": "[]",
		"quoted bool": "{auto: 'false'}", "legacy bool": "{auto: yes}", "bool case": "{auto: True}",
		"quoted concurrency": "{maxConcurrent: '1'}", "float concurrency": "{maxConcurrent: 1.0}",
		"hex speed": "{bytesPerSecond: 0x400000}", "leading zero": "{bytesPerSecond: 04194304}",
		"overflow": "{bytesPerSecond: 9223372036854775808}", "negative": "{bytesPerSecond: -1}",
		"duplicate": "{auto: false, auto: true}", "null child": "{bytesPerSecond: null}",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parse(t, minimalYAML+"hashing: "+field+"\n"); err == nil || strings.Contains(err.Error(), "PRIVATE-SENTINEL") {
				t.Fatalf("invalid hashing configuration accepted or leaked: %v", err)
			}
		})
	}
}
