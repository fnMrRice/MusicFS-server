// Command musicfs-plugins runs one built-in plugin per process.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/fnMrRice/MusicFS-server/plugins/unified"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, input io.ReadCloser, output io.WriteCloser, stderr io.Writer) int {
	worker, err := unified.Select(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if err = worker(ctx, input, output); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(stderr, "Plugin worker failed")
		return 1
	}
	return 0
}
