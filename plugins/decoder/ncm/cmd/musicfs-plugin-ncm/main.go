package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

func main() {
	pluginlog.External()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_ = ncmdecoder.RunWorker(ctx, os.Stdin, os.Stdout)
}
