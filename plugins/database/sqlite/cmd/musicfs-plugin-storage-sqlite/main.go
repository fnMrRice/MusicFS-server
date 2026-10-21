package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/sdk/pluginlog"
	"github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

func main() {
	pluginlog.External()
	if err := rpcbackend.Serve(context.Background(), os.Stdin, os.Stdout, sqlite.Driver()); err != nil {
		fmt.Fprintln(os.Stderr, "Persistence worker stopped")
		os.Exit(1)
	}
}
