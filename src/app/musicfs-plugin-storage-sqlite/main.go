package main

import (
	"context"
	"fmt"
	"os"

	"github.com/fnMrRice/MusicFS-server/src/storage/rpcbackend"
	"github.com/fnMrRice/MusicFS-server/src/storage/sqlite"
)

func main() {
	if err := rpcbackend.Serve(context.Background(), os.Stdin, os.Stdout, sqlite.Driver()); err != nil {
		fmt.Fprintln(os.Stderr, "Persistence worker stopped")
		os.Exit(1)
	}
}
