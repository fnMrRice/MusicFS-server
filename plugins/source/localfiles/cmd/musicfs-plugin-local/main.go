package main

import (
	"context"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
)

func main() {
	pluginlog.External()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	localfiles.Run(ctx, os.Stdin, os.Stdout)
}
