package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	audioffmpeg.RunWorker(ctx, os.Stdin, os.Stdout)
}
