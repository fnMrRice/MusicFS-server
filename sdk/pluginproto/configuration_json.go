package pluginproto

import "encoding/json"

func (c Configuration) MarshalJSON() ([]byte, error) {
	type plain Configuration
	return json.Marshal(struct {
		plain
		ImportDefaults map[string]any `json:"importDefaults"`
	}{plain: plain(c), ImportDefaults: optionValue(c.ImportDefaults)})
}
