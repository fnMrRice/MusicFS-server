package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/bilibili"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

func main() {
	pluginlog.External()
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "This plugin does not accept command-line arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err := provider.Run(ctx, bilibili.Plugin(bilibili.Options{}), os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Bilibili plugin stopped")
		os.Exit(1)
	}
}
