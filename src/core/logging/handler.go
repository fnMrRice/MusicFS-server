package logging

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
)

type operation struct {
	attrs []slog.Attr
	group string
}
type handler struct {
	manager    *Manager
	operations []operation
	pluginID   string
	grouped    bool
}

func handlerOptions() *slog.HandlerOptions {
	return &slog.HandlerOptions{Level: LevelTrace, ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
		if len(groups) == 0 && a.Key == slog.LevelKey {
			if value, ok := a.Value.Any().(slog.Level); ok && value == LevelTrace {
				return slog.String(slog.LevelKey, "TRACE")
			}
		}
		key := strings.ToLower(a.Key)
		for _, word := range []string{"password", "token", "secret", "credential", "authorization", "cookie"} {
			if strings.Contains(key, word) {
				return slog.String(a.Key, "[REDACTED]")
			}
		}
		return a
	}}
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	// 属性也可能随本次记录传入，因此此处只按最小阈值预筛选。
	h.manager.mu.RLock()
	defer h.manager.mu.RUnlock()
	return !h.manager.closed && level >= h.manager.floor
}

func (h *handler) Handle(ctx context.Context, record slog.Record) error {
	id := h.pluginID
	if id == "" && !h.grouped {
		record.Attrs(func(a slog.Attr) bool {
			if a.Key == "pluginId" && a.Value.Kind() == slog.KindString {
				id = a.Value.String()
			}
			return true
		})
	}
	if !ValidPluginID(id) {
		id = ""
	}
	if !h.manager.enabled(id, record.Level) {
		return nil
	}
	sink, err := h.manager.sink(id)
	if err == nil {
		err = h.apply(sink.handler).Handle(ctx, record)
	}
	if err != nil {
		h.manager.reportFailure()
	}
	if h.manager.console != nil {
		err = errors.Join(err, h.apply(h.manager.console).Handle(ctx, record))
	}
	return err
}

func (h *handler) apply(target slog.Handler) slog.Handler {
	for _, op := range h.operations {
		if op.group != "" {
			target = target.WithGroup(op.group)
		} else {
			target = target.WithAttrs(op.attrs)
		}
	}
	return target
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	copy := *h
	copy.operations = append(slices.Clone(h.operations), operation{attrs: slices.Clone(attrs)})
	if !h.grouped {
		for _, a := range attrs {
			if a.Key == "pluginId" && a.Value.Kind() == slog.KindString {
				copy.pluginID = a.Value.String()
			}
		}
	}
	return &copy
}

func (h *handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	copy := *h
	copy.operations = append(slices.Clone(h.operations), operation{group: name})
	copy.grouped = true
	return &copy
}
