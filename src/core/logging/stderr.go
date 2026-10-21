package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
)

// PluginStderr 只采纳协议约定的诊断字段。未知文本只记录长度，避免外部程序泄露凭据。
func PluginStderr(pluginID, instanceID string) io.WriteCloser {
	return &stderrWriter{logger: slog.Default().With("pluginId", pluginID, "instanceId", instanceID)}
}

type stderrWriter struct {
	mu     sync.Mutex
	logger *slog.Logger
	line   []byte
	count  int
}

func (w *stderrWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		index := bytes.IndexByte(p, '\n')
		end := index
		if end < 0 {
			end = len(p)
		}
		w.count += end
		if len(w.line)+end <= 16<<10 {
			w.line = append(w.line, p[:end]...)
		}
		if index < 0 {
			break
		}
		w.flush()
		p = p[end+1:]
	}
	return n, nil
}

func (w *stderrWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.count > 0 {
		w.flush()
	}
	return nil
}

func (w *stderrWriter) flush() {
	defer func() { w.line = w.line[:0]; w.count = 0 }()
	if w.count == 0 {
		return
	}
	var event struct {
		Event      string `json:"event"`
		Level      string `json:"level"`
		DurationMs int64  `json:"durationMs"`
		ErrorCode  string `json:"errorCode"`
	}
	if w.count == len(w.line) && json.Unmarshal(w.line, &event) == nil {
		message := map[string]string{
			"plugin.diagnostic":          "Plugin diagnostic",
			"plugin.operation.started":   "Plugin operation started",
			"plugin.operation.completed": "Plugin operation completed",
			"plugin.operation.failed":    "Plugin operation failed",
			"plugin.lifecycle.started":   "Plugin worker started",
			"plugin.lifecycle.stopping":  "Plugin worker stopping",
			"plugin.lifecycle.stopped":   "Plugin worker stopped",
		}[event.Event]
		if message != "" {
			level := slog.LevelDebug
			switch event.Level {
			case "TRACE", "DEBUG-4":
				level = LevelTrace
			case "INFO":
				level = slog.LevelInfo
			case "WARN":
				level = slog.LevelWarn
			case "ERROR":
				level = slog.LevelError
			}
			attrs := []any{"event", event.Event, "transport", "stderr"}
			if event.DurationMs >= 0 && event.DurationMs <= 24*60*60*1000 {
				attrs = append(attrs, "durationMs", event.DurationMs)
			}
			if code := safeDiagnosticCode(event.ErrorCode); code != "" {
				attrs = append(attrs, "errorCode", code)
			}
			w.logger.Log(context.Background(), level, message, attrs...)
			return
		}
	}
	w.logger.Warn("Plugin emitted unstructured diagnostic output", "bytes", w.count, "content", "[REDACTED]")
}

func safeDiagnosticCode(code string) string {
	switch code {
	case "ACCESS_DENIED", "AUTH_REQUIRED", "CANCELED", "CONFIGURATION_REQUIRED", "DEADLINE_EXCEEDED", "EOF", "INTERNAL_ERROR", "INVALID_ARGUMENT", "NOT_FOUND", "PLATFORM_AUTH_REQUIRED", "PLUGIN_DISABLED", "PRECONDITION_FAILED", "PROTOCOL_MISMATCH", "RATE_LIMITED", "RESOURCE_CHANGED", "SOURCE_OFFLINE", "TEMPORARY_FAILURE", "UNSUPPORTED":
		return code
	default:
		return ""
	}
}
