package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_ = ncmdecoder.RunWorker(ctx, os.Stdin, os.Stdout)
}
