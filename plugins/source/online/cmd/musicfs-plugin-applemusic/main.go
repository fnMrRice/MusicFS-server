package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/applemusic"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

func main() {
	if len(os.Args) != 1 {
		fmt.Fprintln(os.Stderr, "This plugin does not accept command-line arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_ = provider.Run(ctx, applemusic.Plugin(applemusic.Options{}), os.Stdin, os.Stdout)
}
