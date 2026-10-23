package pluginproto

import (
	"bytes"
	"encoding/json"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

// ScanOverrides 将单次刷新动作与可保存的目录导入设置分开。
type ScanOverrides struct {
	ImportOverrides
	RefreshMetadata bool `json:"refreshMetadata,omitempty"`
}

func (o *ScanOverrides) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fault.Fail("INVALID_ARGUMENT", "Scan options must be an object")
	}
	var result ScanOverrides
	if value, exists := fields["refreshMetadata"]; exists {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &result.RefreshMetadata) != nil {
			return fault.Fail("INVALID_ARGUMENT", "Metadata refresh must be a boolean")
		}
		delete(fields, "refreshMetadata")
	}
	options, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(options, &result.ImportOverrides); err != nil {
		return err
	}
	*o = result
	return nil
}
