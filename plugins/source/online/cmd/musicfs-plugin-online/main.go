// Command musicfs-plugin-online runs one selected online adapter per process.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/source/online/unified"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/sdk/provider"
)

func main() {
	pluginlog.External()
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.ReadCloser, output io.WriteCloser, stderr io.Writer) int {
	plugin, err := unified.Select(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	_ = provider.Run(ctx, plugin, input, output)
	return 0
}
