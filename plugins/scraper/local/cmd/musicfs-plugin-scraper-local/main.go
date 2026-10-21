package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

func main() {
	pluginlog.External()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_ = scraperlocal.RunWorker(ctx, os.Stdin, os.Stdout)
}
