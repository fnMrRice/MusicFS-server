package playback

import (
	"context"
	"errors"
	"log/slog"

	"github.com/fnMrRice/MusicFS-server/sdk/pluginproto"
	"github.com/fnMrRice/MusicFS-server/src/core/domain"
)

func logPlaybackFailure(ctx context.Context, message string, err error, attrs ...any) {
	level := slog.LevelWarn
	code := pluginproto.ErrorCode(err)
	var known *domain.Error
	if errors.As(err, &known) {
		code = known.Code
	}
	if errors.Is(err, context.Canceled) {
		level, code = slog.LevelDebug, "CANCELED"
	}
	slog.Log(ctx, level, message, append(attrs, "errorCode", code)...)
}
