package pluginproto

import "encoding/json"

// 在线配置复用有界、本地 Schema 校验，不允许远程引用或把未知默认值混入客户端。
func validateOnlineConfiguration(d OnlineDescriptor) error {
	configuration := Configuration{SchemaRevision: d.SchemaRevision, ImportOptionsSchema: d.ConfigSchema}
	schema, err := configuration.schema()
	if err != nil {
		return err
	}
	raw, err := json.Marshal(d.ConfigDefaults)
	if err != nil || len(raw) > 32<<10 {
		return Failure("PROTOCOL_MISMATCH", "Invalid online configuration defaults")
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || schema.Validate(value) != nil {
		return Failure("PROTOCOL_MISMATCH", "Online defaults violate the declared configuration schema")
	}
	return nil
}
