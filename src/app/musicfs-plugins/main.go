// Command musicfs-plugins runs one built-in plugin per process.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/src/online"
	"github.com/fnMrRice/MusicFS-server/src/plugins/audioffmpeg"
	"github.com/fnMrRice/MusicFS-server/src/plugins/localfiles"
	"github.com/fnMrRice/MusicFS-server/src/plugins/ncmdecoder"
	"github.com/fnMrRice/MusicFS-server/src/plugins/provideronline"
	"github.com/fnMrRice/MusicFS-server/src/plugins/scraperlocal"
)

func main() {
	role := flag.String("plugin", "", "Plugin role: local, audio-ffmpeg, ncm, scraper-local, netease, qqmusic, applemusic")
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
	case online.Netease, online.QQMusic, online.AppleMusic:
		_ = provideronline.Run(ctx, *role, os.Stdin, os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, "A supported --plugin role is required")
		os.Exit(2)
	}
}
