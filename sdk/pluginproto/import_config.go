package pluginproto

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// ImportOverrides preserves the difference between omission and an explicit zero.
type ImportOverrides struct {
	Recursive       *bool     `json:"recursive,omitempty" yaml:"recursive,omitempty"`
	MaxDepth        *int      `json:"maxDepth,omitempty" yaml:"maxDepth,omitempty"`
	ImportLyrics    *bool     `json:"importLyrics,omitempty" yaml:"importLyrics,omitempty"`
	ExcludePatterns *[]string `json:"excludePatterns,omitempty" yaml:"excludePatterns,omitempty"`
	SkipHidden      *bool     `json:"skipHidden,omitempty" yaml:"skipHidden,omitempty"`
	RespectNoMedia  *bool     `json:"respectNoMedia,omitempty" yaml:"respectNoMedia,omitempty"`
}

func (p ImportOverrides) Clone() *ImportOverrides {
	result := p
	if p.SkipHidden != nil {
		value := *p.SkipHidden
		result.SkipHidden = &value
	}
	if p.RespectNoMedia != nil {
		value := *p.RespectNoMedia
		result.RespectNoMedia = &value
	}
	if p.ExcludePatterns != nil {
		value := append([]string{}, (*p.ExcludePatterns)...)
		result.ExcludePatterns = &value
	}
	if p.Recursive != nil {
		value := *p.Recursive
		result.Recursive = &value
	}
	if p.MaxDepth != nil {
		value := *p.MaxDepth
		result.MaxDepth = &value
	}
	if p.ImportLyrics != nil {
		value := *p.ImportLyrics
		result.ImportLyrics = &value
	}
	return &result
}

func (p *ImportOverrides) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil || raw == nil {
		return fault.Fail("INVALID_ARGUMENT", "Import options must be an object")
	}
	for _, value := range raw {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fault.Fail("INVALID_ARGUMENT", "Import options cannot contain null")
		}
	}
	type plain ImportOverrides
	var result plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return fault.Fail("INVALID_ARGUMENT", "Invalid import options")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fault.Fail("INVALID_ARGUMENT", "Invalid import options")
	}
	*p = ImportOverrides(result)
	return nil
}

func MergeImportOptions(base ImportOptions, patch ImportOverrides) (ImportOptions, error) {
	result := base
	if patch.SkipHidden != nil {
		result.SkipHidden = *patch.SkipHidden
	}
	if patch.RespectNoMedia != nil {
		result.RespectNoMedia = *patch.RespectNoMedia
	}
	result.ExcludePatterns = append([]string{}, base.ExcludePatterns...)
	if patch.ExcludePatterns != nil {
		result.ExcludePatterns = append([]string{}, (*patch.ExcludePatterns)...)
	}
	if patch.Recursive != nil {
		result.Recursive = *patch.Recursive
		if patch.MaxDepth == nil {
			if !result.Recursive {
				result.MaxDepth = 0
			} else if !base.Recursive {
				result.MaxDepth = 0
			}
		}
	}
	if patch.MaxDepth != nil {
		result.MaxDepth = *patch.MaxDepth
	}
	if patch.ImportLyrics != nil {
		result.ImportLyrics = *patch.ImportLyrics
	}
	if err := result.Validate(); err != nil {
		return ImportOptions{}, err
	}
	var err error
	result.ExcludePatterns, err = CanonicalExclusions(result.ExcludePatterns)
	return result, err
}

type UnavailableOption struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type Configuration struct {
	SchemaRevision      string                       `json:"schemaRevision"`
	ImportOptionsSchema map[string]any               `json:"importOptionsSchema"`
	ImportDefaults      ImportOptions                `json:"importDefaults"`
	UnavailableOptions  map[string]UnavailableOption `json:"unavailableOptions"`
}
