package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/fnMrRice/MusicFS-server/src/core/auth"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
	"github.com/fnMrRice/MusicFS-server/src/core/logging"
)

// RegisterLogging 注册管理员日志等级接口；修改仅作用于当前服务进程。
func (s *Server) RegisterLogging(manager *logging.Manager) {
	s.Handle("GET /api/v1/admin/logging", true, false, requireAdmin(func(w http.ResponseWriter, r *http.Request, _ auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		return WriteJSON(w, http.StatusOK, manager.Settings())
	}))
	s.Handle("PUT /api/v1/admin/logging", true, true, requireAdmin(func(w http.ResponseWriter, r *http.Request, session auth.Session) error {
		if _, err := onlineQuery(r); err != nil {
			return err
		}
		var raw json.RawMessage
		if err := DecodeJSON(w, r, &raw); err != nil {
			return err
		}
		settings, err := decodeLoggingSettings(raw)
		if err != nil {
			return err
		}
		if err := manager.Configure(settings); err != nil {
			return domain.Fail("INVALID_ARGUMENT", "Invalid logging levels or plugin identifiers")
		}
		s.options.Logger.InfoContext(r.Context(), "Logging levels updated", "userId", session.User.ID, "configuredLevel", settings.Level, "pluginOverrides", len(settings.Plugins))
		return WriteJSON(w, http.StatusOK, manager.Settings())
	}))
}

func decodeLoggingSettings(raw []byte) (logging.Settings, error) {
	settings := logging.Settings{Plugins: map[string]string{}}
	fields, err := loggingObject(raw)
	if err != nil || len(fields) != 2 || fields["level"] == nil || fields["plugins"] == nil {
		return settings, domain.Fail("INVALID_ARGUMENT", "Logging update requires level and plugins only")
	}
	if err := json.Unmarshal(fields["level"], &settings.Level); err != nil || logging.ValidateLevel(settings.Level) != nil {
		return settings, domain.Fail("INVALID_ARGUMENT", "Log level must be trace, debug, info, warn or error")
	}
	plugins, err := loggingObject(fields["plugins"])
	if err != nil || len(plugins) > 128 {
		return settings, domain.Fail("INVALID_ARGUMENT", "plugins must be an object with at most 128 log level overrides")
	}
	for id, rawLevel := range plugins {
		var level string
		if err := json.Unmarshal(rawLevel, &level); err != nil || !logging.ValidPluginID(id) || logging.ValidateLevel(level) != nil {
			return settings, domain.Fail("INVALID_ARGUMENT", "Invalid plugin identifier or log level")
		}
		settings.Plugins[id] = level
	}
	return settings, nil
}

// loggingObject 拒绝重复键，避免不同 JSON 消费者对覆盖配置产生不同解释。
func loggingObject(raw []byte) (map[string]json.RawMessage, error) {
	invalid := domain.Fail("INVALID_ARGUMENT", "Logging settings must contain JSON objects without duplicate keys")
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, invalid
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || fields[key] != nil {
			return nil, invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, invalid
		}
		fields[key] = value
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || decoder.Decode(new(any)) != io.EOF {
		return nil, invalid
	}
	return fields, nil
}
