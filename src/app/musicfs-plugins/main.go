// Command musicfs-plugins runs one built-in plugin per process.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ffmpeg"
	"github.com/fnMrRice/MusicFS-server/plugins/decoder/ncm"
	"github.com/fnMrRice/MusicFS-server/plugins/scraper/local"
	"github.com/fnMrRice/MusicFS-server/plugins/source/localfiles"
)

func main() {
	role := flag.String("plugin", "", "Plugin role: local, audio-ffmpeg, ncm, scraper-local")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "Unexpected positional arguments")
		os.Exit(2)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	// The role is fixed before opening the protocol channel. Business requests
	// cannot switch the platform or plugin implementation of a running process.
	switch *role {
	case "local":
		localfiles.Run(ctx, os.Stdin, os.Stdout)
	case "audio-ffmpeg":
		audioffmpeg.RunWorker(ctx, os.Stdin, os.Stdout)
	case "ncm":
		_ = ncmdecoder.RunWorker(ctx, os.Stdin, os.Stdout)
	case "scraper-local":
		_ = scraperlocal.RunWorker(ctx, os.Stdin, os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, "A supported --plugin role is required")
		os.Exit(2)
	}
}
