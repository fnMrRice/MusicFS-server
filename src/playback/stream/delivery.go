package stream

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
)

// deliveryWriter 保留 ServeContent 的写入错误，并记录不含底层地址或路径的诊断。
type deliveryWriter struct {
	http.ResponseWriter
	controller *http.ResponseController
	ctx        context.Context
	sessionID  string
	bytes      int64
	err        error
}

func (w *deliveryWriter) Write(data []byte) (int, error) {
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	w.record("write", err)
	return n, err
}

func (w *deliveryWriter) FlushError() error {
	err := w.controller.Flush()
	w.record("flush", err)
	return err
}

func (w *deliveryWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *deliveryWriter) record(stage string, err error) {
	if err == nil || w.err != nil {
		return
	}
	w.err = err
	reason, level := "io_error", slog.LevelWarn
	var networkError net.Error
	if w.ctx.Err() != nil {
		reason, level = "canceled", slog.LevelDebug
	} else if errors.As(err, &networkError) && networkError.Timeout() {
		reason = "timeout"
	}
	slog.Log(w.ctx, level, "Playback stream delivery failed", "sessionId", w.sessionID, "stage", stage, "reason", reason, "bytes", w.bytes)
}
