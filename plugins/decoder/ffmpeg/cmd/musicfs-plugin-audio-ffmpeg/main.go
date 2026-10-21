package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

func main() {
	pluginlog.External()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	audioffmpeg.RunWorker(ctx, os.Stdin, os.Stdout)
}
