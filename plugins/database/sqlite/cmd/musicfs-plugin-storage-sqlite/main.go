package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fnMrRice/MusicFS-server/plugins/database/sqlite"
	"github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
)

func main() {
	if err := rpcbackend.Serve(context.Background(), os.Stdin, os.Stdout, sqlite.Driver()); err != nil {
		fmt.Fprintln(os.Stderr, "Persistence worker stopped")
		os.Exit(1)
	}
}
