package pluginproto

import (
	"encoding/json"
	"fmt"
	"regexp"

	"github.com/fnMrRice/MusicFS-server/src/domain"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

var configurationID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// Configuration permits a bounded, local JSON Schema subset. No URI references,
// content loaders, executable expressions, or arbitrary extension keywords exist.
func (c *Configuration) schema() (*jsonschema.Schema, error) {
	if c == nil || !configurationID.MatchString(c.SchemaRevision) {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid configuration revision")
	}
	b, err := json.Marshal(c)
	if err != nil || len(b) > 32<<10 {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: configuration exceeds limit")
	}
	var schema map[string]any
	b, _ = json.Marshal(c.ImportOptionsSchema)
	if err := json.Unmarshal(b, &schema); err != nil || schema == nil {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid configuration schema")
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: import options must reject unknown properties")
	}
	if err := boundedSchema(schema, 0); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	const resource = "https://musicfs.invalid/internal/plugin-import-options"
	if err := compiler.AddResource(resource, schema); err != nil {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid configuration schema")
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		return nil, fmt.Errorf("PROTOCOL_MISMATCH: invalid configuration schema")
	}
	return compiled, nil
}

func boundedSchema(schema map[string]any, depth int) error {
	fail := func() error { return fmt.Errorf("PROTOCOL_MISMATCH: unsupported configuration schema") }
	if depth > 8 || len(schema) > 20 {
		return fail()
	}
	for key, value := range schema {
		switch key {
		case "type":
			if value != "object" && value != "boolean" && value != "integer" && value != "string" && value != "array" {
				return fail()
			}
		case "title", "description":
			text, ok := value.(string)
			if !ok || len(text) > 1024 {
				return fail()
			}
		case "properties":
			properties, ok := value.(map[string]any)
			if !ok || len(properties) > 32 {
				return fail()
			}
			for name, child := range properties {
				object, ok := child.(map[string]any)
				if !configurationID.MatchString(name) || !ok {
					return fail()
				}
				if err := boundedSchema(object, depth+1); err != nil {
					return err
				}
			}
		case "if", "then", "else", "items":
			child, ok := value.(map[string]any)
			if !ok {
				return fail()
			}
			if err := boundedSchema(child, depth+1); err != nil {
				return err
			}
		case "required":
			values, ok := value.([]any)
			if !ok || len(values) > 32 {
				return fail()
			}
			for _, item := range values {
				if name, ok := item.(string); !ok || !configurationID.MatchString(name) {
					return fail()
				}
			}
		case "additionalProperties":
			if value != false {
				return fail()
			}
		case "minimum", "maximum", "minLength", "maxLength", "minItems", "maxItems":
			if _, ok := value.(float64); !ok {
				return fail()
			}
		case "default", "const":
			switch value.(type) {
			case bool, string, float64:
			case []any:
				items := value.([]any)
				if len(items) > MaxExcludePatterns {
					return fail()
				}
				for _, item := range items {
					text, ok := item.(string)
					if !ok || len(text) > MaxExcludePatternBytes {
						return fail()
					}
				}
			default:
				return fail()
			}
		case "enum":
			values, ok := value.([]any)
			if !ok || len(values) > 32 {
				return fail()
			}
			for _, item := range values {
				switch item.(type) {
				case bool, string, float64:
				default:
					return fail()
				}
			}
		case "uniqueItems":
			if value != true {
				return fail()
			}
		default:
			return fail()
		}
	}
	return nil
}

func optionValue(options ImportOptions) map[string]any {
	b, _ := json.Marshal(options)
	var value map[string]any
	_ = json.Unmarshal(b, &value)
	return value
}

func (c *Configuration) Validate() error {
	schema, err := c.schema()
	if err != nil {
		return err
	}
	if err := c.ImportDefaults.Validate(); err != nil {
		return fmt.Errorf("PROTOCOL_MISMATCH: invalid import defaults")
	}
	if err := schema.Validate(optionValue(c.ImportDefaults)); err != nil {
		return fmt.Errorf("PROTOCOL_MISMATCH: defaults violate configuration schema")
	}
	for key, option := range c.UnavailableOptions {
		if !configurationID.MatchString(key) || option.Code != "UNSUPPORTED" || len(option.Reason) < 1 || len(option.Reason) > 1024 {
			return fmt.Errorf("PROTOCOL_MISMATCH: invalid unavailable option")
		}
		if value, exists := optionValue(c.ImportDefaults)[key]; !exists || value != false {
			return fmt.Errorf("PROTOCOL_MISMATCH: unavailable option must default to false")
		}
	}
	if c.UnavailableOptions == nil {
		return fmt.Errorf("PROTOCOL_MISMATCH: unavailable options must be an object")
	}
	return nil
}

func (c *Configuration) ValidateOptions(options ImportOptions) error {
	if err := options.Validate(); err != nil {
		return err
	}
	schema, err := c.schema()
	if err != nil {
		return err
	}
	value := optionValue(options)
	if err := schema.Validate(value); err != nil {
		return domain.Fail("INVALID_ARGUMENT", "Import options do not match the plugin schema")
	}
	for key := range c.UnavailableOptions {
		if value[key] == true {
			return domain.Fail("UNSUPPORTED", "Requested import option is not available")
		}
	}
	return nil
}
