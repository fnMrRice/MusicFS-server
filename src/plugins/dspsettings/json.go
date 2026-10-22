package dspsettings

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// UnmarshalJSON 保持严格对象边界；JSON null 不得被解码为数值参数的零值。
func (o *Options) UnmarshalJSON(data []byte) error {
	type plain Options
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	var wire struct {
		Chain []struct {
			Params json.RawMessage `json:"params"`
		} `json:"chain"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	for _, node := range wire.Chain {
		if bytes.Equal(bytes.TrimSpace(node.Params), []byte("null")) {
			return fmt.Errorf("DSP params must be an object")
		}
		var parameters map[string]json.RawMessage
		if len(node.Params) != 0 {
			if err := json.Unmarshal(node.Params, &parameters); err != nil {
				return err
			}
		}
		for _, raw := range parameters {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return fmt.Errorf("DSP parameters must be numbers")
			}
		}
	}
	*o = Options(value)
	return nil
}
