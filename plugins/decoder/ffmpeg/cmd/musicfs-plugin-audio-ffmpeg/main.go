package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	audioffmpeg.RunWorker(ctx, os.Stdin, os.Stdout)
}
