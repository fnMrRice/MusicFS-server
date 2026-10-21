// Package pluginlog 为插件提供不包含业务载荷的结构化诊断日志。
package pluginlog

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/fnMrRice/MusicFS-server/sdk/fault"
)

const LevelTrace = slog.Level(-8)

// External 将独立插件的日志写到标准错误，标准输出保留给协议。
func External() {
	slog.SetDefault(slog.New(externalHandler{slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: LevelTrace})}))
}

// 统一标记自有结构化诊断，宿主可忽略自由文本并保留原始等级。
type externalHandler struct{ slog.Handler }

func (h externalHandler) Handle(ctx context.Context, record slog.Record) error {
	hasEvent := false
	record.Attrs(func(attr slog.Attr) bool {
		hasEvent = hasEvent || attr.Key == "event"
		return !hasEvent
	})
	if !hasEvent {
		record = record.Clone()
		record.AddAttrs(slog.String("event", "plugin.diagnostic"))
	}
	return h.Handler.Handle(ctx, record)
}

func (h externalHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return externalHandler{h.Handler.WithAttrs(attrs)}
}

func (h externalHandler) WithGroup(name string) slog.Handler {
	return externalHandler{h.Handler.WithGroup(name)}
}

type contextKey struct{}

// Context 绑定宿主已验证的插件身份；不会把账号、路径或请求内容写入日志。
func Context(ctx context.Context, pluginID, instanceID string) context.Context {
	logger := slog.Default().With("pluginId", pluginID)
	if instanceID != "" {
		logger = logger.With("instanceId", instanceID)
	}
	return context.WithValue(ctx, contextKey{}, logger)
}

func Logger(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(contextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

// Operation 只记录固定操作名、耗时及白名单错误码，调用者不应传入用户文本。
func Operation(ctx context.Context, operation string) func(error) {
	logger := Logger(ctx)
	started := time.Now()
	logger.Log(ctx, LevelTrace, "Plugin operation started", "event", "plugin.operation.started", "operation", operation)
	return func(err error) {
		elapsed := time.Since(started).Milliseconds()
		if err == nil {
			logger.DebugContext(ctx, "Plugin operation completed", "event", "plugin.operation.completed", "operation", operation, "durationMs", elapsed)
			return
		}
		code := ErrorCode(err)
		level := slog.LevelWarn
		switch code {
		case "CANCELED", "EOF", "NOT_FOUND", "UNSUPPORTED", "INVALID_ARGUMENT", "PLATFORM_AUTH_REQUIRED", "AUTH_REQUIRED", "PRECONDITION_FAILED", "RATE_LIMITED", "CONFLICT", "IDEMPOTENCY_EXPIRED", "CURSOR_EXPIRED":
			level = slog.LevelDebug
		}
		logger.Log(ctx, level, "Plugin operation failed", "event", "plugin.operation.failed", "operation", operation, "durationMs", elapsed, "errorCode", code)
	}
}

// ErrorCode 永远不返回异常原文，防止第三方错误携带凭据或私有路径。
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "DEADLINE_EXCEEDED"
	}
	if errors.Is(err, io.EOF) {
		return "EOF"
	}
	var detail *fault.Error
	code, _, _ := strings.Cut(err.Error(), ":")
	if errors.As(err, &detail) {
		code = detail.Code
	}
	switch code {
	case "ACCESS_DENIED", "AUTH_REQUIRED", "CONFIGURATION_REQUIRED", "CONFLICT", "CURSOR_EXPIRED", "IDEMPOTENCY_EXPIRED", "INVALID_ARGUMENT", "NOT_FOUND", "PLATFORM_AUTH_REQUIRED", "PLUGIN_DISABLED", "PRECONDITION_FAILED", "PROTOCOL_MISMATCH", "RATE_LIMITED", "RESOURCE_CHANGED", "SOURCE_OFFLINE", "TEMPORARY_FAILURE", "UNSUPPORTED":
		return code
	default:
		return "INTERNAL_ERROR"
	}
}
